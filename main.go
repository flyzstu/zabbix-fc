package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/goccy/go-json"
)

var (
	logger      *slog.Logger
	loggerLevel *slog.LevelVar
	fcURL       string
	fcPassword  string
	fcUser      string
	fcToken     string
	debugFlag   bool
)

var hostMetrics = []string{
	"cpu_usage",
	"mem_usage",
	"nic_byte_in",
	"nic_byte_out",
	"disk_io_in",
	"disk_io_out",
	"logic_disk_usage",
	"vm_mem_usage",
	"vm_mem_total",
	"vm_mem_free",
	"vm_run_num",
	"hosts_vio_in",
	"hosts_vio_out",
	"hosts_vbyte_in",
	"hosts_vbyte_out",
}

var vmMetrics = []string{
	"cpu_usage",
	"mem_usage",
	"mem_free",
	"disk_usage",
	"nic_byte_in",
	"nic_byte_out",
	"nic_byte_in_out",
	"disk_io_in",
	"disk_io_out",
}

type CustomTransport struct {
	T http.RoundTripper
}

func (ct *CustomTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if fcToken == "" {
		return nil, fmt.Errorf("token is empty")
	}
	// 设置你想要的全局 headers
	req.Header.Add("X-Auth-Token", fcToken)
	req.Header.Add("Accept", "application/json;version=8.1;charset=UTF-8")
	req.Header.Add("Content-Type", "application/json; charset=UTF-8")

	// 调用默认的 RoundTripper
	return ct.T.RoundTrip(req)
}

func init() {

	// 添加一个日志等级
	loggerLevel = new(slog.LevelVar)
	// 设置日志等级
	loggerLevel.Set(slog.LevelError)
	// 根据这个日志等级创建日志处理器
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: loggerLevel,
	}))

	flag.StringVar(&fcURL, "fcURL", "", "fcURL")
	flag.StringVar(&fcUser, "fcUser", "", "fcUser")
	flag.StringVar(&fcPassword, "fcPassword", "", "fcPassword")
	flag.BoolVar(&debugFlag, "debug", false, "log debug flag")
}

func main() {
	flag.Parse()
	// 根据flag动态创建日志等级
	if debugFlag {
		loggerLevel.Set(slog.LevelInfo)
	}
	logger.Info("hello", "your", "code")
	if fcURL == "" || fcUser == "" || fcPassword == "" {
		logger.Error("oops", "command failed", "命令初始化失败")
		return
	}
	logger.Info("命令初始化成功")

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	sessionURL, _ := url.JoinPath(fcURL, "session")
	req, err := http.NewRequest("POST", sessionURL, nil)
	if err != nil {
		logger.Error("oops", err.Error(), "请求创建失败")
		return
	}
	req.Header.Add("Accept", "application/json;version=8.1;charset=UTF-8")
	req.Header.Add("Content-Type", "application/json; charset=UTF-8")
	req.Header.Add("Accept-Language", "zh_CN")
	req.Header.Add("X-Auth-User", fcUser)
	req.Header.Add("X-Auth-Key", fcPassword)
	req.Header.Add("X-Auth-UserType", "0")

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("oops", err.Error(), "登录请求发送失败")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logger.Error("oops", resp.Status, "登录失败，请检查用户名或密码")
		return
	}

	fcToken = resp.Header.Get("X-Auth-Token")
	logger.Info("登录成功，获取token成功", "token", fcToken)

	// 获取站点信息

	baseURL, _ := url.JoinPath(fcURL, "service")
	siteURL, _ := url.JoinPath(baseURL, "sites")

	commonClient := &http.Client{
		Transport: &CustomTransport{T: tr},
	}
	// resp, err = commonClient.Get(siteURL)
	// if err != nil {
	// 	logger.Error("oops", err.Error(), "siteURL请求失败")
	// 	return
	// }
	siteIdURL, _ := url.JoinPath(siteURL, "1") // 选择第一个站点

	// 获取当前站点的全部主机
	// 1. 构造请求
	hostURL, _ := url.JoinPath(siteIdURL, "hosts")
	hostURLObj, err := url.Parse(hostURL)
	if err != nil {
		logger.Error("oops", err.Error(), "hostURL构造失败")
		return
	}
	params := url.Values{}
	params.Add("limit", "100")
	params.Add("offset", "0")
	queryString := params.Encode()
	hostURLObj.RawQuery = queryString
	// 创建监控
	resp, err = commonClient.Get(hostURLObj.String())
	if err != nil {
		logger.Error("oops", err.Error(), "hostURL请求失败")
		return
	}
	defer resp.Body.Close()

	var respObj RespList
	err = json.NewDecoder(resp.Body).Decode(&respObj)
	if err != nil {
		panic(err)
	}
	// 获取主机列表
	logger.Info("获取主机列表成功", "主机列表数目", fmt.Sprint(len(respObj.Hosts)))

	for _, host := range respObj.Hosts {
		logger.Info("主机信息", "主机标识", host.Urn, "主机地址", host.Name, "IP", host.IP)
	}

	// 构造json请求
	var L2Obj = MonitorReqList{}
	for _, host := range respObj.Hosts {
		L2Obj = append(L2Obj, MonitorElement{
			Urn:      host.Urn,
			MetricID: hostMetrics,
		})
	}
	var newBuffer bytes.Buffer
	err = json.NewEncoder(&newBuffer).Encode(L2Obj)
	if err != nil {
		logger.Error("oops", "构造监控请求的消息体失败", err.Error())
		return
	}

	monitorURL, _ := url.JoinPath(siteIdURL, "monitors")
	monitorrealtimeURL, _ := url.JoinPath(monitorURL, "realtimedata")
	resp, err = commonClient.Post(monitorrealtimeURL, "application/json", &newBuffer)
	if err != nil {
		logger.Error("oops", "监控请求发送失败", err.Error())
		return
	}
	defer resp.Body.Close()

	var monitorRespObj MonitorRespList
	err = json.NewDecoder(resp.Body).Decode(&monitorRespObj)
	if err != nil {
		logger.Error("oops", "解析监控响应失败", err.Error())
		return
	}

	// cpu_usage{id="1"} 1001

	var message strings.Builder

	for _, item := range monitorRespObj.Items {
		for _, value := range item.Value {
			message.WriteString(fmt.Sprintf("%s{name=\"%s\"}=%s\n", value.MetricID, item.ObjectName, value.MetricValue.(string)))
		}
	}

}
