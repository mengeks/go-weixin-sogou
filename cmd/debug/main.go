package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"

	"golang.org/x/net/publicsuffix"
)

func main() {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	client := &http.Client{Jar: jar}

	urls := []string{
		"https://weixin.sogou.com/weixin?ie=utf8&type=1&page=1&query=ConnectEd",
		"https://weixin.sogou.com/weixin?ie=utf8&type=2&page=1&query=ConnectEd",
	}

	for _, u := range urls {
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("URL: %s\nERROR: %v\n\n", u, err)
			continue
		}
		body, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("URL: %s\nStatus: %d\nBody (first 2000 chars):\n%s\n\n---\n", u, resp.StatusCode, string(body[:min(2000, len(body))]))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
