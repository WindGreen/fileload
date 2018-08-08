package main

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/gob"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"
)

const VERSION = "1.0.1"

var queue, redo, finish chan int
var cor, size, length, timeout int
var hash, dst string
var verify bool
var version bool

func main() {
	flag.IntVar(&cor, "c", 1, "coroutine num")
	flag.IntVar(&size, "s", 0, "buff length")
	flag.IntVar(&size, "t", 0, "timeout")
	flag.StringVar(&dst, "f", "file", "file name")
	flag.StringVar(&hash, "h", "sha1", "sha1 or md5 to verify the file")
	flag.BoolVar(&verify, "v", false, "verify file, not download")
	flag.BoolVar(&version, "version", false, "show version")
	flag.Parse()

	url := os.Args[len(os.Args)-1]

	if version {
		fmt.Println("Fileload version:", VERSION)
		return
	}

	if verify {
		file, err := os.Open(url)
		if err != nil {
			log.Println(err)
			return
		}
		if hash == "sha1" {
			h := sha1.New()
			io.Copy(h, file)
			r := h.Sum(nil)
			log.Printf("sha1 of file: %x\n", r)
		} else if hash == "md5" {
			h := md5.New()
			io.Copy(h, file)
			r := h.Sum(nil)
			log.Printf("sha1 of file: %x\n", r)
		}

		return
	}

	startTime := time.Now()

	client := http.Client{}
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal(err)
	}
	response, err := client.Do(request)
	response.Body.Close()
	num := response.Header.Get("Content-Length")
	length, _ = strconv.Atoi(num)
	log.Println("Conetnt-Length", length)
	ranges := response.Header.Get("Accept-Ranges")
	log.Println("Ranges:", ranges)

	if size <= 0 {
		size = int(math.Ceil(float64(length) / float64(cor)))
	}
	fragment := int(math.Ceil(float64(length) / float64(size)))
	queue = make(chan int, cor)
	redo = make(chan int, int(math.Floor(float64(cor)/2)))
	go func() {
		for i := 0; i < fragment; i++ {
			select {
			case j := <-redo:
				queue <- j
			default:
			}
			queue <- i
		}
	}()
	finish = make(chan int, cor)
	for j := 0; j < cor; j++ {
		go Do(request, fragment, j)
	}
	for k := 0; k < fragment; k++ {
		<-finish
	}
	log.Println("Start to combine files...")

	file, err := os.Create(dst)
	if err != nil {
		log.Println(err)
		return
	}
	defer file.Close()
	var offset int64 = 0
	for x := 0; x < fragment; x++ {
		filename := fmt.Sprintf("tmp_%d", x)
		buf, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Println(err)
			continue
		}
		file.WriteAt(buf, offset)
		offset += int64(len(buf))
		os.Remove(filename)
	}
	log.Println("Written to ", dst)
	//hash
	if hash == "sha1" {
		h := sha1.New()
		io.Copy(h, file)
		r := h.Sum(nil)
		log.Printf("sha1 of file: %x\n", r)
	} else if hash == "md5" {
		h := md5.New()
		io.Copy(h, file)
		r := h.Sum(nil)
		log.Printf("sha1 of file: %x\n", r)
	}

	finishTime := time.Now()
	log.Printf("Time:%f\n", finishTime.Sub(startTime).Seconds())
}

func Do(request *http.Request, fragment, no int) {
	var req http.Request
	err := DeepCopy(&req, request)
	if err != nil {
		log.Println("ERROR|prepare request:", err)
		log.Panic(err)
		return
	}
	for {
		i := <-queue
		//log.Printf("[%d][%d]Start download\n",no, i)
		start := i * size
		var end int
		if i < fragment-1 {
			end = start + size - 1
		} else {
			end = length - 1
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		log.Printf("[%d][%d]Start download:%d-%d\n", no, i, start, end)
		cli := http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		}
		resp, err := cli.Do(&req)
		if err != nil {
			log.Printf("[%d][%d]ERROR|do request:%s\n", no, i, err.Error())
			redo <- i
			continue
		}

		//log.Printf("[%d]Content-Length:%s\n", i, resp.Header.Get("Content-Length"))
		log.Printf("[%d][%d]Content-Range:%s\n", no, i, resp.Header.Get("Content-Range"))

		file, err := os.Create(fmt.Sprintf("tmp_%d", i))
		if err != nil {
			log.Printf("[%d][%d]ERROR|create file:%s\n", no, i, err.Error())
			file.Close()
			resp.Body.Close()
			redo <- i
			continue
		}
		log.Printf("[%d][%d]Writing to file...\n", no, i)
		n, err := io.Copy(file, resp.Body)
		if err != nil {
			log.Printf("[%d][%d]ERROR|write to file:%s\n", no, i, err.Error())
			file.Close()
			resp.Body.Close()
			redo <- i
			continue
		}
		log.Printf("[%d][%d]Wrote successfully:%d\n", no, i, n)

		file.Close()
		resp.Body.Close()

		finish <- 1
	}
}

func DeepCopy(dst, src interface{}) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(src); err != nil {
		return err
	}
	return gob.NewDecoder(bytes.NewBuffer(buf.Bytes())).Decode(dst)
}
