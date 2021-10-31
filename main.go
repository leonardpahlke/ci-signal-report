package main

import (
	"fmt"
	"net/http"
	"sync"
)

func main() {
	links := []string{
		"http://abc.com",
		"http://pqr.com",
		"http://xyz.com",
	}
	for e := range makeCalls(links) {
		fmt.Println(e)
	}
}

func makeCalls(links []string) chan string {
	// creating a channel to share string type data
	c := make(chan string)
	// fetching data from each link
	go func() {
		defer close(c)
		var wg sync.WaitGroup
		for _, link := range links {
			wg.Add(1)
			go func(l string) {
				_, err := http.Get(l)
				if err == nil {
					c <- l
				}
				wg.Done()
			}(link)
		}
		wg.Wait()
	}()
	return c
}

/*
loop: func1(LIST)

func1(LIST) CHANNEL
CHANNEL
GO FUNC
DEFER CLOSE CHANNEL
WAITGROUP
WAITGROUP ++
GO FUNC
WAITGROUP DONE
WAITGROUP.WAIT
*/
