package main

func main() {
	py := NewProxy("localhost:9000")
	py.Serve()
}
