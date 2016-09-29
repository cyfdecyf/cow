package main

// data range by first byte
var CNIPDataRange [256]struct {
	start int
	end   int
}

func initCNIPData() {
	n := len(CNIPDataStart)
	var curr uint32 = 0
	var preFirstByte uint32 = 0
	for i := 0; i < n; i++ {
		firstByte := CNIPDataStart[i] >> 24
		if curr != firstByte {
			curr = firstByte
			if preFirstByte != 0 {
				CNIPDataRange[preFirstByte].end = i - 1
			}
			CNIPDataRange[firstByte].start = i
			preFirstByte = firstByte
		}
	}
	CNIPDataRange[preFirstByte].end = n - 1
}
