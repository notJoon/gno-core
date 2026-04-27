// List-backed byte slice fallback in ConvertTo:
// slice literals go through NewListArray, so sBase.Data == nil.
package main

type B byte

func main() {
	s := []B{1, 2, 3, 4}
	a := [3]B(s[1:])
	println(a[0], a[1], a[2])
}

// Output:
// (2 main.B) (3 main.B) (4 main.B)
