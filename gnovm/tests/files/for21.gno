package main

func main() {
    for i := 0; i < 10; i++ {
        if i == 1 {
            _ = func() int {
                continue
                return 11
            }()
        }
        println(i)
    }
    println("wat???")
}

// Error:
// main/for21.gno:7:17-25: continue statement out of place

// TypeCheckError:
// main/for21.gno:7:17: continue not in for statement
