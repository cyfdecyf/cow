// +build generate
// go run chinaip_gen.go

package main

import (
	"bufio"
	"strconv"
	"fmt"
	"log"
	"encoding/binary"
	"net/http"
	"os"
	"strings"
	"net"
	"errors"
)

const (
	apnicFile = "http://ftp.apnic.net/apnic/stats/apnic/delegated-apnic-latest"
)

// ip to long int
func ip2long(ipstr string) (uint32, error) {
	ip := net.ParseIP(ipstr)
	if ip == nil {
		return 0, errors.New("Invalid IP")
	}
	ip = ip.To4()
	if ip == nil {
		return 0, errors.New("Not IPv4")
	}
	return binary.BigEndian.Uint32(ip), nil
}

func main() {
	resp, err := http.Get(apnicFile)
	if err != nil {
		panic(err)
	}
	if resp.StatusCode != 200 {
		panic(fmt.Errorf("Unexpected status %d", resp.StatusCode))
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	
	start_list := []string{}
	count_list := []string{}

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		if strings.ToLower(parts[1]) != "cn" || strings.ToLower(parts[2]) != "ipv4" {
			continue
		}
		ip := parts[3]
		count := parts[4]
		ipLong, err := ip2long(ip)
		if err != nil {
			panic(err)
		}	
		start_list = append(start_list, strconv.FormatUint(uint64(ipLong), 10))
		count_list = append(count_list, count)
	}

	file, err := os.OpenFile("chinaip_data.go", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		log.Fatalf("Failed to generate chinaip_data.go: %v", err)
	}
	defer file.Close()

	fmt.Fprintln(file, "package main")
	fmt.Fprint(file, "var CNIPDataStart = []uint32 {\n	")
	fmt.Fprint(file, strings.Join(start_list, ",\n	"))
	fmt.Fprintln(file, ",\n	}")

	fmt.Fprint(file, "var CNIPDataNum = []uint{\n	")
	fmt.Fprint(file, strings.Join(count_list, ",\n	"))
	fmt.Fprintln(file, ",\n	}")
}
