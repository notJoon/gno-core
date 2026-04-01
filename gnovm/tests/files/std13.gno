package main

import "chain/runtime"

func Register() {
	runtime.AssertOriginCall()
}

func main() {
	fn := func() {
		Register()
	}

	defer func() {
		println(recover())
	}()
	fn()
}

// Output:
// invalid non-origin call
