package main

func main() {
	x := 1
	switch x {
	case 1:
		fallthrough // fallthrough must be the last instruction
		println(1)
	case 2:
		println(2)
	}
}

// Error:
// main/files/switch8c.gno:7:3: fallthrough statement out of place

// TypeCheckError:
// main/files/switch8c.gno:7:3: fallthrough statement out of place
