package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type Device struct {
	UDN      string
	Name     string
	URLBase  string
	CtrlURL  string
	LocalURL string
	Expired  time.Time
}

type CastService struct {
	status            int32
	conn              *net.UDPConn
	devices           map[string]Device
	select_device     Device
	file_path         string
	ffmpeg_process    *exec.Cmd
	productor         int32
	consumer          int32
	flv_header        []byte
	flv_script        []byte
	flv_video         []byte
	buf               chan []byte
	is_need_key_frame bool
}

func (this *CastService) Init() bool {
	if atomic.CompareAndSwapInt32(&this.status, 0, -1) {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(("0.0.0.0"))})
		if err != nil {
			log.Fatalln(err)
			return false
		}
		this.devices = map[string]Device{}
		this.conn = conn
		go this.searchDevices()
		this.flv_header = make([]byte, 9)
		this.buf = make(chan []byte)
		go this.httpServer()
		atomic.StoreInt32(&this.status, 1)
	}
	return false
}

func (this *CastService) searchDevices() {
	req, _ := http.NewRequest("M-SEARCH", "*", nil)
	req.Host = "239.255.255.250:1900"
	req.Header.Set("Man", "\"ssdp:discover\"")
	req.Header.Set("Mx", "5")
	req.Header.Set("ST", "upnp:rootdevice")
	data, _ := httputil.DumpRequest(req, false)
	resp := make([]byte, 2048)
	for {
		this.conn.WriteToUDP(data, &net.UDPAddr{
			Port: 1900,
			IP:   net.ParseIP("239.255.255.250"),
		})
		this.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, addr, err := this.conn.ReadFrom(resp)
		if err == nil && addr.(*net.UDPAddr) != nil {
			c, _ := net.DialUDP("udp", nil, addr.(*net.UDPAddr))
			local_addr := strings.Split(c.LocalAddr().String(), ":")[0]
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
								UDN:      root.UDN,
								Name:     root.Name,
								URLBase:  root.URLBase,
								CtrlURL:  service.CtrlURL,
								LocalURL: local_addr,
								Expired:  time.Now().Add(time.Second * time.Duration(max_age)),
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
		c.File(this.file_path)
	})
	engine.POST("/live", func(c *gin.Context) {
		if atomic.CompareAndSwapInt32(&this.productor, 0, 1) {
			func() {
				defer atomic.StoreInt32(&this.productor, 0)
				tmp_pre_tag_header := make([]byte, 15)
				status := 0
				for {
					var err error = nil
					switch status {
					case 0:
						_, err = io.ReadFull(c.Request.Body, this.flv_header)
						if err != nil {
							break
						}
						status = 1
					case 1:
						_, err = io.ReadFull(c.Request.Body, tmp_pre_tag_header)
						if err != nil {
							break
						}
						if tmp_pre_tag_header[4] != 18 {
							err = fmt.Errorf("not script tag")
							break
						}
						size := Uint32(tmp_pre_tag_header[5:8])
						data := make([]byte, size)
						_, err = io.ReadFull(c.Request.Body, data)
						if err != nil {
							break
						}
						this.flv_script = make([]byte, 11+size)
						copy(this.flv_script, tmp_pre_tag_header[4:])
						copy(this.flv_script[11:], data)
						status = 2
					case 2:
						_, err = io.ReadFull(c.Request.Body, tmp_pre_tag_header)
						if err != nil {
							break
						}
						if tmp_pre_tag_header[4] != 9 {
							err = fmt.Errorf("not video tag")
							break
						}
						size := Uint32(tmp_pre_tag_header[5:8])
						data := make([]byte, size)
						_, err = io.ReadFull(c.Request.Body, data)
						if err != nil {
							break
						}
						this.flv_video = make([]byte, 11+size)
						copy(this.flv_video, tmp_pre_tag_header[4:])
						copy(this.flv_video[11:], data)
						status = 3
					case 3:
						_, err = io.ReadFull(c.Request.Body, tmp_pre_tag_header)
						if err != nil {
							break
						}
						if tmp_pre_tag_header[4] != 9 {
							err = fmt.Errorf("not video tag")
							break
						}
						size := Uint32(tmp_pre_tag_header[5:8])
						data := make([]byte, size)
						_, err = io.ReadFull(c.Request.Body, data)
						if err != nil {
							break
						}
						tag := make([]byte, 11+size)
						copy(tag, tmp_pre_tag_header[4:])
						copy(tag[11:], data)
						is_key := false
						if data[0]>>4 == 1 {
							is_key = true
						}
						if this.consumer == 1 {
							if this.is_need_key_frame && !is_key {
								break
							}
							this.is_need_key_frame = false
							this.buf <- tag
						}
					}
					if err != nil {
						log.Println(err)
						break
					}

				}
			}()
		} else {
			log.Println("Connector Exists")
		}
	})
	engine.GET("/live", func(c *gin.Context) {
		if atomic.LoadInt32(&this.productor) == 1 && atomic.CompareAndSwapInt32(&this.consumer, 0, 1) {
			func() {
				defer func() {
					atomic.StoreInt32(&this.consumer, -1)
					select {
					case <-this.buf:
					case <-time.After(time.Second * 2):
					}
					atomic.StoreInt32(&this.consumer, 0)
				}()

				c.Writer.Header().Add("content-type", "video/x-flv")
				c.Writer.Header().Del("Content-Length")

				pre_tag_size := uint32(0)
				pre_tag_size_buf := make([]byte, 4)
				start_timestamp := uint32(0)

				c.Writer.Write(this.flv_header)

				binary.BigEndian.PutUint32(pre_tag_size_buf, pre_tag_size)
				c.Writer.Write(pre_tag_size_buf)
				c.Writer.Write(this.flv_script)
				pre_tag_size = uint32(len(this.flv_script))

				binary.BigEndian.PutUint32(pre_tag_size_buf, pre_tag_size)
				c.Writer.Write(pre_tag_size_buf)
				c.Writer.Write(this.flv_video)
				pre_tag_size = uint32(len(this.flv_video))

				cancel := false
				this.is_need_key_frame = true
				for !cancel {
					select {
					case b := <-this.buf:
						binary.BigEndian.PutUint32(pre_tag_size_buf, pre_tag_size)
						c.Writer.Write(pre_tag_size_buf)
						if start_timestamp == 0 {
							start_timestamp = Uint32(b[4:7])
						}
						timestamp := Uint32(b[4:7]) - start_timestamp
						binary.BigEndian.PutUint32(pre_tag_size_buf, timestamp)
						copy(b[4:7], pre_tag_size_buf[1:4])
						c.Writer.Write(b)
						pre_tag_size = uint32(len(b))
						c.Writer.Flush()
						pre_tag_size = uint32(len(b))
					case <-c.Request.Context().Done():
						cancel = true
						break
					}
				}
			}()
		} else {
			log.Println("No Input")
		}
	})
	engine.Run(":12345")
}

func (this *CastService) ListDevices() []Device {
	res := []Device{}
	for k, v := range this.devices {
		if v.Expired.Before(time.Now()) {
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

func (this *CastService) setURL(url string) bool {
	type Action struct {
		Xmlns              string `xml:"xmlns:u,attr"`
		InstanceID         int
		CurrentURI         string
		CurrentURIMetaData string
	}
	type Body struct {
		ActionName Action `xml:"u:SetAVTransportURI"`
	}
	type Envelope struct {
		XMLName       xml.Name `xml:"s:Envelope"`
		Xmlns         string   `xml:"xmlns:s,attr"`
		EncodingStyle string   `xml:"s:encodingStyle,attr"`
		Body          Body     `xml:"s:Body"`
	}

	envelop := Envelope{}
	envelop.Xmlns = "http://schemas.xmlsoap.org/soap/envelope/"
	envelop.EncodingStyle = "http://schemas.xmlsoap.org/soap/encoding/"
	envelop.Body.ActionName.Xmlns = "urn:schemas-upnp-org:service:AVTransport:1"
	envelop.Body.ActionName.InstanceID = 0
	envelop.Body.ActionName.CurrentURI = url
	data, err := xml.MarshalIndent(&envelop, " ", "  ")
	if err != nil {
		log.Println(err)
		return false
	}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", this.select_device.URLBase, this.select_device.CtrlURL), bytes.NewReader(data))
	if err != nil {
		log.Println(err)
		return false
	}
	req.Header.Set("Content-Type", "text/xml; charset=\"utf-8\"")
	req.Header.Set("SOAPACTION", "\"urn:schemas-upnp-org:service:AVTransport:1#SetAVTransportURI\"")
	_, err = http.DefaultClient.Do(req)
	if err != nil {
		log.Println(err)
	}
	return true
}

func (this *CastService) stopService() {
	if this.ffmpeg_process != nil {
		this.ffmpeg_process.Process.Kill()
		this.ffmpeg_process.Wait()
		this.ffmpeg_process = nil
	}
}

func (this *CastService) CastFile(path string) bool {
	if atomic.CompareAndSwapInt32(&this.status, 2, -1) {
		this.file_path = path
		this.stopService()
		return func() bool {
			defer func() {
				atomic.StoreInt32(&this.status, 2)
			}()
			return this.setURL(fmt.Sprintf("http://%s:12345/file", this.select_device.LocalURL))
		}()
	}
	return false
}

func (this *CastService) CastScreen(width, height int) bool {
	if atomic.CompareAndSwapInt32(&this.status, 2, -1) {
		this.stopService()
		return func() bool {
			defer func() {
				atomic.StoreInt32(&this.status, 2)
			}()
			if runtime.GOOS == "linux" {
				cmd2 := exec.Command("ffmpeg", "-f", "x11grab", "-s", fmt.Sprintf("%dx%d", width, height), "-r", "30", "-i", ":0.0", "http://127.0.0.1:23456/live")
				cmd2.Start()
				this.ffmpeg_process = cmd2
				return this.setURL(fmt.Sprintf("http://%s:12345/live", this.select_device.LocalURL))
			} else if runtime.GOOS == "windows" {
				cmd2 := exec.Command("ffmpeg", "-f", "gdigrab", "-i", "desktop", "-f", "flv", "http://127.0.0.1:12345/live")
				cmd2.Start()
				this.ffmpeg_process = cmd2
				return this.setURL(fmt.Sprintf("http://%s:12345/live", this.select_device.LocalURL))
			} else {
				return false
			}
		}()
	}
	return false
}

func main() {
	srv := CastService{}
	srv.Init()
	for len(srv.ListDevices()) == 0 {
		time.Sleep(time.Second)
		fmt.Println("test")
	}
	srv.SelectDevice(srv.ListDevices()[0].UDN)
	time.Sleep(3 * time.Second)
	// srv.CastFile("C:\\Users\\Tbolp\\Videos\\【僕の戦争】《进击的巨人》最终章 绝对阳间指弹！.mp4")
	srv.CastScreen(1920, 1080)
	// srv.CastFile("/home/tbolp/Pictures/icon.jpeg")
	// srv.CastFile("/home/tbolp/Videos/Smooth Criminal.mp4")
	wg := sync.WaitGroup{}
	wg.Add(1)
	wg.Wait()
}

func Uint32(b []byte) uint32 {
	_ = b[2]
	return uint32(b[2]) | uint32(b[1])<<8 | uint32(b[0])<<16
}
