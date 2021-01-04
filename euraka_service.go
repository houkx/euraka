package euraka

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type EurekaService struct {
	log                              *log.Logger
	appName                          string
	appNameUPPER                     string
	ip                               string
	host                             string
	port                             int
	eurekaUrls                       []string
	nextUrlIndex                     int
	leaseExpirationDurationInSeconds int
	heartBeatTimeGap                 time.Duration
	scheduleTimer                    *time.Timer
	heartbeatUrl                     string
	contentType                      string
	isUnregistered                   bool
	logDebug                         bool
	ctx                              context.Context
}


func NewEurekaService(appName string, port int, eurekaUrls [] string,
	leaseExpirationDurationInSeconds int,
	ctx context.Context) *EurekaService {
	http.DefaultClient.Timeout = time.Duration(2 * time.Second)
	ip := localIp()
	timeGap := time.Duration(leaseExpirationDurationInSeconds) * time.Second
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ip
	}
	return &EurekaService{
		log:                              log.New(os.Stdout, "INFO ", log.Lshortfile|log.LstdFlags),
		appName:                          appName,
		appNameUPPER:                     strings.ToUpper(appName),
		ip:                               ip,
		host:                             hostname,
		port:                             port,
		eurekaUrls:                       eurekaUrls,
		nextUrlIndex:                     0,
		isUnregistered:                   false,
		logDebug:                         false,
		leaseExpirationDurationInSeconds: leaseExpirationDurationInSeconds,
		heartBeatTimeGap:                 timeGap,
		scheduleTimer:                    time.NewTimer(timeGap),
		ctx:                              ctx,
		contentType:                      `application/json;charset=UTF-8`,
	}
}


func (p *EurekaService) SetLogger(logger *log.Logger) {
	p.log = logger
}
func (p *EurekaService) SetLogDebug(logDebug bool) {
	p.logDebug = logDebug
}

/**
  * 注册到服务注册中心
  */
func (p *EurekaService) Register() bool {
	var startTime = fmt.Sprint(time.Now().Unix() * 1000)
	var tpl = template
	appName := p.appName
	appNameUPPER := p.appNameUPPER
	host := p.host
	ip := p.ip
	tpl = strings.Replace(tpl, "${app}", appName, -1)
	tpl = strings.Replace(tpl, "${APP}", appNameUPPER, -1)
	tpl = strings.Replace(tpl, "${ip}", ip, -1)
	tpl = strings.Replace(tpl, "${port}", fmt.Sprint(p.port), -1)
	tpl = strings.Replace(tpl, "${securePortEnable}", "false", -1)
	tpl = strings.Replace(tpl, "${securePort}", "8443", -1)
	tpl = strings.Replace(tpl, "${hostName}", host, -1)
	tpl = strings.Replace(tpl, "${timestamp}", startTime, -1)
	tpl = strings.Replace(tpl, "${leaseExpirationDurationInSeconds}", fmt.Sprint(p.leaseExpirationDurationInSeconds), -1)
	// 注册之前先注销
	p.Unregister()
	var urlAddress = p.eurekaUrl() + "apps/" + appNameUPPER
	resp, err := http.DefaultClient.Post(urlAddress, p.contentType, bytes.NewBuffer([]byte(tpl)))
	var code = -1
	var msg = ""
	if resp != nil {
		code = resp.StatusCode
		msg = resp.Status
	}
	if err != nil {
		p.log.Println("register error:", err)
		p.nextUrl()
	} else {
		p.log.Printf("register code:%d ,msg=%s\t%s\n%s\n", code, msg, urlAddress, tpl)
	}
	var instanceId = host + ":" + appName
	var uri = "apps/" + appNameUPPER + "/" + instanceId
	p.heartbeatUrl = p.eurekaUrl() + uri + "?status=UP&lastDirtyTimestamp=" + startTime
	if p.isUnregistered {
		p.isUnregistered = false
		p.tryHeartbeat()
	}
	return code == 204
}

// 从服务注册中心删除
func (p *EurekaService) Unregister() bool {
	p.isUnregistered = true
	var instanceId = p.host + ":" + p.appName
	var uri = "apps/" + p.appNameUPPER + "/" + instanceId
	var urlAddress = p.eurekaUrl() + uri
	client := http.DefaultClient
	req, err := http.NewRequest("DELETE", urlAddress, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", p.contentType)
	resp, err := client.Do(req)
	var code = -1
	var msg = ""
	if resp != nil {
		code = resp.StatusCode
		msg = resp.Status
	}
	p.log.Printf("注销:%d-%s %s\n", code, msg, urlAddress)
	if err != nil {
		return false
	}
	return code == 200
}

func (p *EurekaService) tryHeartbeat() {
	if p.isUnregistered {
		return
	}
	go func() {
		for ; !p.isUnregistered; {
			select {
			case <-p.ctx.Done():
				p.isUnregistered = true
				break
			case <-p.scheduleTimer.C:
				if hbOk, _ := p.heartbeat(); !hbOk {
					msg := "成功"
					var ok = p.Register()
					if !ok {
						msg = "失败"
					}
					p.log.Println("注册", msg)
				}
				p.scheduleTimer.Reset(p.heartBeatTimeGap)
			}
		}
		p.log.Println("Stopped scheduleTimer.")
	}()
}

// 向注册中心发送心跳
func (p *EurekaService) heartbeat() (bool, error) {
	client := http.DefaultClient
	req, err := http.NewRequest("PUT", p.heartbeatUrl, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", p.contentType)
	resp, err := client.Do(req)
	if p.logDebug {
		var code = -1
		var msg = ""
		if resp != nil {
			code = resp.StatusCode
			msg = resp.Status
		}
		p.log.Printf("心跳:%d-%s %s\n", code, msg, p.heartbeatUrl)
	}
	if err != nil {
		return false, err
	}
	return resp.StatusCode == 200, nil
}
func (p *EurekaService) nextUrl() {
	var next = p.nextUrlIndex + 1
	if next >= len(p.eurekaUrls) {
		next = 0
	}
	p.nextUrlIndex = next
}
func (p *EurekaService) eurekaUrl() string {
	return p.eurekaUrls[p.nextUrlIndex]
}
func localIp() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, value := range addrs {
		if ipnet, ok := value.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

const template = `{
  "instance": {
    "instanceId":"${hostName}:${app}",
    "hostName":"${ip}",
    "app":"${APP}",
    "isCoordinatingDiscoveryServer":"false",
    "ipAddr":"${ip}",
    "vipAddress":"${app}",
    "secureVipAddress": "${app}",
    "status":"UP",
    "overriddenStatus":"UNKNOWN",
    "countryId":1,
    "lastUpdatedTimestamp":"${timestamp}",
    "lastDirtyTimestamp":"${timestamp}",
    "port":{
      "@enabled": "true",
      "$": "${port}"
    },
    "securePort":{"@enabled":"${securePortEnable}", "$":"${securePort}"},
    "homePageUrl" : "http://${ip}:${port}/",
    "statusPageUrl": "http://${ip}:${port}/info",
    "healthCheckUrl": "http://${ip}:${port}/isok",
    "dataCenterInfo" : {
      "@class": "com.netflix.appinfo.InstanceInfo$DefaultDataCenterInfo",
      "name": "MyOwn"
    },"leaseInfo":{
      "renewalIntervalInSecs": 1,
      "durationInSecs": ${leaseExpirationDurationInSeconds},
      "registrationTimestamp": 0,
      "lastRenewalTimestamp": 0,
      "evictionTimestamp": 0,
      "serviceUpTimestamp": 0
    }
  }
}
`
