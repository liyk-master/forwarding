package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

type UdpStr struct {
	C    string  `json:"C"`
	T    int     `json:"T"`
	P    float32 `json:"P"`
	L    float32 `json:"L"`
	H    float32 `json:"H"`
	O    float32 `json:"O"`
	V    int     `json:"V"`
	Buy  float32 `json:"Buy"`
	Sell float32 `json:"Sell"`
	LC   float32 `json:"LC"`
	ZD   float32 `json:"ZD"`
	ZDF  float32 `json:"ZDF"`
}

type GlobalData struct {
	mu          sync.Mutex
	requestData string
}

type authState int

const (
	stateWaitingUsername authState = iota
	stateWaitingPassword
	stateAuthenticated
)

func main() {
	tcpAddr, err := net.ResolveTCPAddr("tcp4", ":8090")
	if err != nil {
		fmt.Println("Error resolving TCP address:", err)
		return
	}

	tcp, err := net.ListenTCP("tcp4", tcpAddr)
	if err != nil {
		fmt.Println("Error listening to TCP:", err)
		return
	}

	defer tcp.Close()

	fmt.Println("Server started...")
	for {
		conn, err := tcp.Accept()
		if err != nil {
			fmt.Println("Error accepting TCP connection:", err)
			continue
		}
		fmt.Println("Client connected:", conn.RemoteAddr())
		var globalData GlobalData
		go handleClient(conn, &globalData)
	}
}

func handleClient(conn net.Conn, globalData *GlobalData) {
	defer conn.Close()
	_, err := conn.Write([]byte("> Universal DDE Connector 9.00\r\n> Copyright 1999-2008 MetaQuotes Software Corp.\r\n> Login: "))
	if err != nil {
		fmt.Println("TCP connect err：", err)
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("send Message err：", err)
	}
	fmt.Println(string(buf[:n]))
	userNamePre := regexp.MustCompile(`\busername`)
	if userNamePre.MatchString(string(buf[:n])) {
		userName := strings.TrimPrefix(string(buf[:n]), "username_")
		userName = strings.ReplaceAll(userName, "\r\n", "")
		if userName != "root" {
			_, _ = conn.Write([]byte("username is not found!"))
			conn.Close()
			return
		}
		_, _ = conn.Write([]byte("> Password: "))
		data := make([]byte, 1024)
		n, err = conn.Read(data)
		if err != nil {
			fmt.Println("send Message err：", err)
			return
		}
		fmt.Println("Password is :", string(data[:n]))
		// 验证密码
		passWordPre := regexp.MustCompile(`\bpassword`)
		if passWordPre.MatchString(string(data[:n])) {
			passWord := strings.TrimPrefix(string(data[:n]), "password_")
			passWord = strings.ReplaceAll(passWord, "\r\n", "")
			if passWord != "63a9f0ea7bb98050796b64" {
				_, _ = conn.Write([]byte("wrong password!"))
				conn.Close()
				return
			}
			_, _ = conn.Write([]byte("> Access granted\r\n"))
			udpSendMessage(conn, globalData)
		}
	}
}

func udpSendMessage(conn net.Conn, globalData *GlobalData) {
	stopChan := make(chan struct{})
	tcpChan := make(chan string)
	go func() {
		for {
			data := make([]byte, 1024)
			n, err := conn.Read(data)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 连接超时
					fmt.Println("Connection timeout.")
					close(stopChan)
					break
				} else if err == io.EOF {
					// 连接被远程服务器关闭
					fmt.Println("Connection closed by remote server.")
					close(stopChan)
					break
				} else {
					// 其他错误
					fmt.Println("Error reading from TCP:", err)
					close(stopChan)
					break
				}
			}
			tcpChan <- string(data[:n])
		}
	}()
	udpAddr, err := net.ResolveUDPAddr("udp4", ":8091")
	if err != nil {
		fmt.Println("Error resolving UDP address:", err)
		return
	}

	udp, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		fmt.Println("Error listening to UDP:", err)
		return
	}
	defer udp.Close()
	buf := make([]byte, 1024)
	revChan := make(chan string)
	go func() {
		for {
			len, _, err := udp.ReadFromUDP(buf)

			if err != nil {
				if err == io.EOF {
					return
				}
				fmt.Println("Error reading from UDP:", err)
				return
			}
			revChan <- string(buf[:len])
		}
	}()
	for {
		select {
		case udpDataStr := <-revChan:
			var udpData UdpStr
			if err = json.Unmarshal([]byte(udpDataStr), &udpData); err != nil {
				fmt.Println("json Unmarshal err：", err)
				break
			}
			globalData.mu.Lock()
			if udpData.C == "ACAG" {
				globalData.requestData = fmt.Sprintf("%s %.3f %.3f\r\n", udpData.C, udpData.Buy, udpData.Sell)
			} else if udpData.C == "ZFAG" {
				globalData.requestData = fmt.Sprintf("%s %.3f %.3f\r\n", udpData.C, udpData.Buy, udpData.Sell)
			} else {
				globalData.requestData = fmt.Sprintf("%s %.2f %.2f\r\n", udpData.C, udpData.Buy, udpData.Sell)
			}
			globalData.mu.Unlock()
			currentTime := time.Now()
			formattedTime := currentTime.Format("2006-01-02 15:04:05")
			t := time.Unix(int64(udpData.T), 0).Format("2006-01-02 15:04:05")
			// 改客户端需要的数据
			_, err = conn.Write([]byte(globalData.requestData))
			if err != nil {
				//close(stop)
				fmt.Println("Error Writing from TCP:", err)
				break
			}
			// 日志
			fmt.Printf("client_ip：%s service_time：%s data_time：%s requestData：%s", conn.RemoteAddr(), formattedTime, t, globalData.requestData)
		case tcpDataStr := <-tcpChan:
			fmt.Println("tcpData：", tcpDataStr)
		case <-stopChan:
			fmt.Println("tcp sending stop!")
			return
		default:

		}
	}
}
