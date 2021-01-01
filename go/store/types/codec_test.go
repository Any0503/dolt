// Copyright 2019 Dolthub, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// This file incorporates work covered by the following copyright and
// permission notice:
//
// Copyright 2017 Attic Labs, Inc. All rights reserved.
// Licensed under the Apache License, version 2.0:
// http://www.apache.org/licenses/LICENSE-2.0

package types

import (
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodecWriteFloat(t *testing.T) {
	test := func(f float64, exp []byte) {
		w := newBinaryNomsWriter()
		w.writeFloat(Float(f), Format_7_18)
		assert.Equal(t, exp, w.data())
	}

	// We use zigzag encoding for the signed bit. For positive n we do 2*n and for negative we do 2*-n - 1
	test(0, []byte{0, 0}) //  0 * 2 **  0

	test(1, []byte{1 * 2, 0})            //  1 * 2 **  0
	test(2, []byte{1 * 2, 1 * 2})        //  1 * 2 **  1
	test(-2, []byte{(1 * 2) - 1, 1 * 2}) // -1 * 2 **  1
	test(.5, []byte{1 * 2, 1*2 - 1})     //  1 * 2 ** -1
	test(-.5, []byte{1*2 - 1, 1*2 - 1})  // -1 * 2 ** -1
	test(.25, []byte{1 * 2, 2*2 - 1})    //  1 * 2 ** -2
	test(3, []byte{3 * 2, 0})            // 0b11 * 2 ** 0

	test(15, []byte{15 * 2, 0})     // 0b1111 * 2**0
	test(256, []byte{1 * 2, 8 * 2}) // 1 * 2*8
	test(-15, []byte{15*2 - 1, 0})  // -15 * 2*0
}

func TestCodecReadFloat(t *testing.T) {
	test := func(data []byte, exp float64) {
		r := binaryNomsReader{buff: data}
		n := r.readFloat(Format_7_18)
		assert.Equal(t, exp, n)
		assert.Equal(t, len(data), int(r.offset))
	}

	test([]byte{0, 0}, 0) //  0 * 2 **  0

	test([]byte{1 * 2, 0}, 1)           //  1 * 2 **  0
	test([]byte{1 * 2, 1 * 2}, 2)       //  1 * 2 **  1
	test([]byte{1*2 - 1, 1 + 1}, -2)    // -1 * 2 **  1
	test([]byte{1 * 2, 1*2 - 1}, .5)    //  1 * 2 ** -1
	test([]byte{1*2 - 1, 1*2 - 1}, -.5) // -1 * 2 ** -1
	test([]byte{1 * 2, 2*2 - 1}, .25)   //  1 * 2 ** -2
	test([]byte{3 * 2, 0}, 3)           // 0b11 * 2 ** 0

	test([]byte{15 * 2, 0}, 15)     // 0b1111 * 2**0
	test([]byte{1 * 2, 8 * 2}, 256) // 1 * 2*8
	test([]byte{15*2 - 1, 0}, -15)  // -15 * 2*0
}

func TestUnrolledDecode(t *testing.T) {
	const NumDecodes = 100000
	masks := []uint64{
		0xFF,
		0xFFFF,
		0xFFFFFF,
		0xFFFFFFFF,
		0xFFFFFFFFFF,
		0xFFFFFFFFFFFF,
		0xFFFFFFFFFFFFFF,
		0xFFFFFFFFFFFFFFFF}

	buf := make([]byte, 10)
	r := rand.New(rand.NewSource(0))
	for i := 0; i < NumDecodes; i++ {
		expectedVal := r.Uint64() & masks[i%8]
		expectedSize := binary.PutUvarint(buf, expectedVal)

		res, size := unrolledDecodeUVarint(buf)
		require.Equal(t, expectedSize, size)
		require.Equal(t, expectedVal, res)
	}

	for i := 0; i < NumDecodes; i++ {
		//non-negative
		expectedVal := int64(uint64(r.Int63()) & masks[i%8])
		expectedSize := binary.PutVarint(buf, expectedVal)

		res, size := unrolledDecodeVarint(buf)
		require.Equal(t, expectedSize, size)
		require.Equal(t, expectedVal, res)

		// negative
		expectedVal = -expectedVal
		expectedSize = binary.PutVarint(buf, expectedVal)

		res, size = unrolledDecodeVarint(buf)
		require.Equal(t, expectedSize, size)
		require.Equal(t, expectedVal, res)
	}
}

func BenchmarkUnrolledDecodeUVarint(b *testing.B) {
	const DecodesPerTest = 1000000

	type ve struct {
		val      uint64
		encoding []byte
	}

	toDecode := make([]ve, b.N*DecodesPerTest)

	r := rand.New(rand.NewSource(0))
	for i := 0; i < b.N*DecodesPerTest; i++ {
		var min uint64
		for j := 0; j < i%9; j++ {
			min = min<<7 | 0x80
		}

		val := min + r.Uint64()&0x7f
		buf := make([]byte, 9)
		size := binary.PutUvarint(buf, val)
		require.Equal(b, i%9+1, size, "%d. min: %x, val: %x, expected_size: %d, size: %d", i, min, val, i%9+1, size)

		toDecode[i] = ve{val, buf}
	}

	type result struct {
		size int
		val  uint64
	}

	decodeBenchmark := []struct {
		name       string
		decodeFunc func([]byte) (uint64, int)
		results    []result
	}{
		{"binary.UVarint", binary.Uvarint, make([]result, len(toDecode))},
		{"unrolled", unrolledDecodeUVarint, make([]result, len(toDecode))},
		//{"denwc.varint.UVarint", varint.Uvarint, make([]result, len(toDecode))},
		//{"noBranch", varuintNoBranch, make([]result, len(toDecode))},
	}

	b.ResetTimer()
	for _, decodeBench := range decodeBenchmark {
		b.Run(decodeBench.name, func(b *testing.B) {
			for i, valAndEnc := range toDecode {
				val, size := decodeBench.decodeFunc(valAndEnc.encoding)
				decodeBench.results[i] = result{size, val}
			}
		})
	}
	b.StopTimer()

	for _, decodeBench := range decodeBenchmark {
		for i, valAndEnc := range toDecode {
			assert.Equal(b, valAndEnc.val, decodeBench.results[i].val)
			assert.Equal(b, i%9+1, decodeBench.results[i].size)
		}
	}
}

/*func varuintNoBranch(buf []byte) (uint64, int) {
	var b uint64
	var x uint64
	var count uint64
	more := uint64(1)

	for i, shift := 0, 0; i < 9; i, shift = i+1, shift+7 {
		count += more
		b = uint64(buf[i])
		x |= more * ((b & 0x7f) << shift)
		more &= (b & 0x80) >> 7
	}

	retCount := int(count) * (-2*int(more) + 1)
	return x, retCount
}*/
