package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

const (
	url = "https://viblo.asia/editors-choice"
	errUnexpectedResponse = "unexpected response: %s"
)

type HTTPClient struct{}

var (
	HttpClient = HTTPClient{}
)

var backoffSchedule = []time.Duration{
	10 * time.Second,
	15 * time.Second,
	20 * time.Second,
	25 * time.Second,
	30 * time.Second,
}

func (c HTTPClient) GetRequest(pathURL string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", pathURL, nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	c.info(fmt.Sprintf("GET %s -> %d", pathURL, resp.StatusCode))
	if resp.StatusCode != 200 {
		respErr := fmt.Errorf(errUnexpectedResponse, resp.Status)
		fmt.Sprintf("request failed: %v", respErr)
		return nil, respErr
	}
	return resp, nil
}

func (c HTTPClient) GetRequestWithRetries (url string) (*http.Response, error){
	var body *http.Response
	var err error
	for _, backoff := range backoffSchedule {
		body, err = c.GetRequest(url)
		if err == nil {
			break
		}
		fmt.Fprintf(os.Stderr, "Request error: %+v\n", err)
		fmt.Fprintf(os.Stderr, "Retrying in %v\n", backoff)
		time.Sleep(backoff)
	}

	// All retries failed
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c HTTPClient) info(msg string) {
	log.Printf("[client] %s\n", msg)
}

func checkError(err error) {
	if err != nil {
		log.Println(err)
	}
}

type Data struct {
	Title string `json:"title"`
	Link string `json:"link"`
}

func totalPage() int {
	response, err := HttpClient.GetRequestWithRetries(url)
	checkError(err)
	defer response.Body.Close()
	doc, err := goquery.NewDocumentFromReader(response.Body)
	checkError(err)

	lastPageLink, _ := doc.Find("ul.pagination li:nth-last-child(2) a").Attr("href") // Đọc dữ liệu từ thẻ a của ul.pagination
	fmt.Println(lastPageLink)
	split := strings.Split(lastPageLink, "=")[1]
	totalPages, _ := strconv.Atoi(split)
	fmt.Println("totalPage->", totalPages)
	return totalPages
}

func onePage(pathURL string) ([]Data, error) {
	response, err := HttpClient.GetRequestWithRetries(pathURL)
	checkError(err)
	defer response.Body.Close()
	doc, err := goquery.NewDocumentFromReader(response.Body)
	checkError(err)

	dataList := make([]Data, 0)

	doc.Find(".post-feed .link").Each(func(i int, s *goquery.Selection) {
		var data Data
		href, _ := s.Attr("href")

		if s.Text() != "" {
			data.Title = s.Text()
			data.Link = "https://viblo.asia" + href
			dataList = append(dataList, data)
		}
	})

	return dataList, nil
}

func allPage() {
	fileName := "viblo_editors_choice.csv"
	file, err := os.Create(fileName)
	if err != nil {
		log.Fatalf("Cloud not create %s", fileName)
	}

	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	writer.Write([]string{"Title", "Link"})

	sem := semaphore.NewWeighted(int64(runtime.NumCPU())) // Tạo ra số lượng group goroutines bằng 8 lần số luồng CPU, cùng đồng thời đi thu thập thông tin
	group, ctx := errgroup.WithContext(context.Background())
	var totalResults int = 0
	totalPage := totalPage()
	fmt.Println("Total CPU:", runtime.NumCPU())

	for page := 1; page <= totalPage; page ++ { // Lặp qua từng trang đã được phân trang
		pathURL := fmt.Sprintf("https://viblo.asia/editors-choice?page=%d", page) // Tìm ra url của từng trang bằng cách nối chuỗi với số trang
		err := sem.Acquire(ctx, 1)
		if err != nil {
			fmt.Printf("Acquire err = %+v\n", err)
			continue
		}
		group.Go(func() error {
			defer sem.Release(1)
			// do work
			dataList, err := onePage(pathURL) // Thu thập thông tin web qua url của page
			if err != nil {
				log.Println(err)
			}
			totalResults += len(dataList)
			for _, data := range dataList {
				writer.Write([]string{data.Title, data.Link})
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil { // Error Group chờ đợi các group goroutines done, nếu có lỗi thì trả về
		fmt.Printf("g.Wait() err = %+v\n", err)
	}
	fmt.Println("crawler done!")
	fmt.Println("total results:", totalResults)
}

func main() {
	allPage()
}