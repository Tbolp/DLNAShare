package main

import (
	"DLANShare/lib"
	"fmt"
	"os"
	"strconv"
	"time"
)

func main() {
	srv := lib.CastService{}
	srv.Init()
	params := map[string]string{}
	for i, v := range os.Args {
		switch v {
		case "-high":
			params[v] = ""
		case "-l":
			params[v] = ""
		case "-i":
			params[v] = os.Args[i+1]
		case "-n":
			params[v] = os.Args[i+1]
		case "-s":
			params[v] = ""
		case "-f":
			params[v] = os.Args[i+1]
		case "-w":
			params[v] = os.Args[i+1]
		case "-h":
			params[v] = os.Args[i+1]
		}
	}
	if _, ok := params["-l"]; ok {
		for {
			devices := srv.ListDevices()
			fmt.Println(devices)
			for _, device := range devices {
				fmt.Printf("id:%s name:%s", device.UDN, device.Name)
			}
			time.Sleep(time.Second)
		}
	}
	try_count := 5
	if v, ok := params["-i"]; ok {
		for try_count > 0 {
			if srv.SelectDevice(v) != nil {
				try_count--
				time.Sleep(time.Second)
			} else {
				break
			}
		}
		if try_count == 0 {
			fmt.Println("No Such Device")
			return
		}
	} else if v, ok := params["-n"]; ok {
		for try_count > 0 {
			if srv.SelectDeviceByName(v) != nil {
				try_count--
				time.Sleep(time.Second)
			} else {
				break
			}
		}
		if try_count == 0 {
			fmt.Println("No Such Device")
			return
		}
	} else {
		for try_count > 0 {
			devices := srv.ListDevices()
			if len(devices) == 0 || srv.SelectDevice(devices[0].UDN) != nil {
				try_count--
				time.Sleep(time.Second)
			} else {
				break
			}
		}
		if try_count == 0 {
			fmt.Println("No Such Device")
			return
		}
	}
	if v, ok := params["-f"]; ok {
		srv.CastFile(v)
	} else {
		width := 800
		height := 600
		var err error
		if v, ok := params["-w"]; ok {
			width, err = strconv.Atoi(v)
			if err != nil {
				fmt.Println(err)
				return
			}
		}
		if v, ok := params["-h"]; ok {
			height, err = strconv.Atoi(v)
			if err != nil {
				fmt.Println(err)
				return
			}
		}
		high := false
		if _, ok := params["-high"]; ok {
			high = true
		}
		srv.CastScreen(width, height, high)
	}
	for {
		status := srv.GetStatus()
		if status == 3 || status == 4 {
			time.Sleep(time.Second)
		} else {
			return
		}
	}
}
