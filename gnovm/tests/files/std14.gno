package main

import "chain/runtime"

func Register(username string) {
	runtime.AssertOriginCall()
}

func notPanics(f func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	f()
	return false
}

func main() {
	panicked := notPanics(func() {
		Register("alice")
	})
	println("anon call panicked:", panicked)

	panicked = notPanics(func() {
		Register("bob")
	})
	println("direct call panicked:", panicked)
}

// Output:
// anon call panicked: true
// direct call panicked: true
