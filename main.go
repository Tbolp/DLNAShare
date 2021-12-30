package main

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type Device struct {
	UDN     string
	Name    string
	URLBase string
	CtrlURL string
	Expired time.Time
}

type CastService struct {
	status        int32
	conn          *net.UDPConn
	devices       map[string]Device
	group         sync.WaitGroup
	is_search     bool
	select_device Device
}

func (this *CastService) Init() {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(("0.0.0.0"))})
	if err != nil {
		log.Fatalln(err)
		return
	}
	this.devices = map[string]Device{}
	this.conn = conn
	this.is_search = true
	go this.searchDevices()
	this.group.Add(1)
	atomic.StoreInt32(&this.status, 1)
}

func (this *CastService) Stop() {
	this.is_search = false
	this.group.Wait()
}

func (this *CastService) searchDevices() {
	defer func() {
		this.group.Done()
	}()
	req, _ := http.NewRequest("M-SEARCH", "*", nil)
	req.Host = "239.255.255.250:1900"
	req.Header.Set("Man", "\"ssdp:discover\"")
	req.Header.Set("Mx", "5")
	req.Header.Set("ST", "upnp:rootdevice")
	data, _ := httputil.DumpRequest(req, false)
	resp := make([]byte, 2048)
	for this.is_search {
		this.conn.WriteToUDP(data, &net.UDPAddr{
			Port: 1900,
			IP:   net.ParseIP("239.255.255.250"),
		})
		this.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _, err := this.conn.ReadFrom(resp)
		if err == nil {
			httpresp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(resp)), nil)
			if err != nil {
				continue
			}
			max_age := 100
			fmt.Sscanf(httpresp.Header.Get("CACHE-CONTROL"), "max-age=%d", &max_age)
			httpresp, err = http.DefaultClient.Get(httpresp.Header.Get("LOCATION"))
			if err != nil {
				continue
			}
			type XMLServices struct {
				Type    string `xml:"serviceType"`
				SCPDURL string `xml:SCPDURL`
				CtrlURL string `xml:"controlURL"`
			}
			type XMLRoot struct {
				Name     string        `xml:"device>friendlyName"`
				UDN      string        `xml:"device>UDN"`
				Services []XMLServices `xml:"device>serviceList>service"`
				URLBase  string        `xml:URLBase`
			}
			root := XMLRoot{}
			body, err := ioutil.ReadAll(httpresp.Body)
			if err != nil {
				continue
			}
			err = xml.Unmarshal(body, &root)
			if err != nil {
				continue
			}
			for _, service := range root.Services {
				if service.Type == "urn:schemas-upnp-org:service:AVTransport:1" {
					var res *http.Response
					res, err = http.DefaultClient.Get(fmt.Sprintf("%s/%s", root.URLBase, service.SCPDURL))
					if err == nil {
						content, _ := ioutil.ReadAll(res.Body)
						re, _ := regexp.Compile("SetAVTransportURI")
						if re.Find(content) != nil {
							dev := Device{
								UDN:     root.UDN,
								Name:    root.Name,
								URLBase: root.URLBase,
								CtrlURL: service.CtrlURL,
								Expired: time.Now().Add(time.Second * time.Duration(max_age)),
							}
							if v, ok := this.devices[root.UDN]; ok {
								if v.Expired.After(time.Now()) {
									this.devices[root.UDN] = dev
								}
							} else {
								this.devices[root.UDN] = dev
							}
						}
					}
				}
			}
		}
	}
}

func (this *CastService) httpServer() {
	engine := gin.Default()
	engine.GET("/file", func(c *gin.Context) {

	})
	engine.Run(":")
}

func (this *CastService) ListDevices() []Device {
	res := []Device{}
	for k, v := range this.devices {
		if v.Expired.After(time.Now()) {
			delete(this.devices, k)
		}
		res = append(res, v)
	}
	return res
}

func (this *CastService) SelectDevice(UDN string) bool {
	if atomic.CompareAndSwapInt32(&this.status, 1, -1) {
		if v, ok := this.devices[UDN]; ok {
			this.select_device = v
			atomic.StoreInt32(&this.status, 2)
			return true
		}
	}
	atomic.StoreInt32(&this.status, 1)
	return false
}

func (this *CastService) CastFile(path string) {

}

func main() {
	srv := CastService{}
	srv.Init()
	srv.Stop()
}
