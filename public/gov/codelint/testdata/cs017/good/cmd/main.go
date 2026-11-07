// A command under the apps tree: a process of its own, with no instance.
package main

func main() {
	go func() {}()
}
