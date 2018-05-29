package main

import (
	"crawler/db"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

type myQueue struct {
	q []string
	m sync.Mutex
}

func (q *myQueue) push(x string) {
	(*q).m.Lock()
	(*q).q = append((*q).q, x)
	(*q).m.Unlock()
}

func (q *myQueue) pop() string {
	(*q).m.Lock()
	x := (*q).q[0]
	if len((*q).q) == 1 {
		(*q).q = []string{}
	} else {
		(*q).q = (*q).q[1:]
	}
	(*q).m.Unlock()
	return x
}

func (q *myQueue) debug() {
	fmt.Println("******QUEUE*********")
	(*q).m.Lock()
	for _, x := range (*q).q {
		fmt.Println(x)
	}
	(*q).m.Unlock()
	fmt.Println("********************")
}

func (q *myQueue) isEmpty() bool {
	if len((*q).q) == 0 {
		return true
	}
	return false
}

type YCrawler struct {
	queue       *myQueue
	debug_level int
	seed_url    string
	domain      string
	base_url    string
	visited     map[string]int
	dbi         *db.DbInstance
}

func (crl *YCrawler) Log(message string, level int) {
	if level <= crl.debug_level {
		fmt.Println(message)
	}
}

func (crl *YCrawler) normalizeURL(link, url string) string {
	var normalized_url string = link
	if strings.HasPrefix(link, "//") {
		normalized_url = strings.Split(link, ":")[0] + "://" + link
	} else if strings.HasPrefix(link, "/") {
		normalized_url = crl.base_url + link
	} else if strings.HasPrefix(link, "http") {
		normalized_url = link
	} else {
		normalized_url = url + link
	}
	return strings.Split(normalized_url, "#")[0]
}

func (crl *YCrawler) Crawl() {
	crl.queue.push(crl.seed_url)
	crl.Log("crawl: running on "+crl.domain+", seed_url is "+crl.seed_url, 0)
	for {
		if crl.queue.isEmpty() {
			crl.Log("crawl: The queue is empty!", 0)
			return
		}
		url := crl.queue.pop()
		//fmt.Println("crawl: Popped ", url)
		//queue.debug()
		urlsch := make(chan string)
		go func() {
			//fmt.Println(url)
			crl.Fetch(url, urlsch)
		}()

		func(c chan string) {
			for x := range c {
				//fmt.Println("crawl: Pushing url ", x)
				crl.queue.push(x)
			}
		}(urlsch)
	}
}

func (crl *YCrawler) Fetch(url string, c chan string) {
	if _, ok := crl.visited[url]; ok {
		crl.Log("fetch: "+url+" visited", 2)
		close(c)
		return
	}
	crl.Log("fetching "+url, 1)
	crl.visited[url] = 1
	urls := crl.collectUrls(url)
	for _, s := range urls {
		c <- s
	}
	close(c)
	return
}

func (crl *YCrawler) collectUrls(lnk string) []string {
	doc, err := goquery.NewDocument(lnk)
	if err != nil {
		crl.Log("Cannot fetch url "+lnk+": "+err.Error(), 2)
		return []string{}
	}
	var urls []string
	doc.Find("*").Each(
		func(i int, item *goquery.Selection) {
			var (
				ok           bool
				link         string
				form_found   bool = false
				form_method  string
				form_enctype string
			)

			check_attrs := []string{"href", "src", "action"}
			post_params := []string{}

			// For each HTML element check attributes which can contain an URL
			// If we found athe "action" attribute we parse parameters
			// We don't want to send any more requests to this page in a way
			// to parse form inputs, so it should be done now.
			for _, tag := range check_attrs {
				if link, ok = item.Attr(tag); ok {
					if tag == "action" {
						form_found = true
						form_method, _ = item.Attr("method")
						form_enctype, _ = item.Attr("enctype")
						item.Find("input").Each(func(i int, x *goquery.Selection) {
							input_name, _ := x.Attr("name")
							//input_type, _ := x.Attr("type")
							post_params = append(post_params, input_name)
						})
					}
					break
				}
			}

			// Element has no interesting attributes or they are empty.
			if len(link) == 0 {
				return
			}

			normalized_url := crl.normalizeURL(link, lnk)

			if crl.isSameDomain(normalized_url) && !crl.isStaticURL(normalized_url) {
				u, err := url.Parse(normalized_url)
				if err != nil {
					panic(err)
				}

				get_params := crl.extractParams(u)
				crl.addParamsToDB(get_params, u.Path, "GET")

				if form_found {
					crl.Log("The form action = "+link+" method "+form_method+", enctype "+form_enctype+" found", 1)
					crl.addParamsToDB(post_params, u.Path, form_method)
				}

				crl.Log("\t--> "+normalized_url, 3)
				urls = append(urls, normalized_url)
			} else {
				crl.Log("Same host restriction for foreign url "+normalized_url, 3)
			}
		})
	return urls
}

func (crl *YCrawler) isSameDomain(link string) bool {
	var sameDomainRegexp = regexp.MustCompile(`^https?:\/\/` + crl.domain + `.*$`)
	return sameDomainRegexp.MatchString(link)
}

func (crl *YCrawler) isStaticURL(link string) bool {
	var rxStatic = regexp.MustCompile(`(.*\.zip)|(.*\.js)|(.*\.css)|(.*\.pdf)|(.*\.woff)|(.*\.jpg)|(.*\.jpeg)|(.*\.doc.*)|(.*\.gif)|(.*\.png)|(.*\.avi)|(.*\.mpeg.*)|(.*\.mpg.*)`)
	return rxStatic.MatchString(link)
}

func (crl *YCrawler) addParamsToDB(params []string, path string, p_type string) {
	if len(params) == 0 {
		return
	}
	domain_id := crl.dbi.GetDomainId(crl.domain)
	path_id := crl.dbi.GetPathId(domain_id, path)

	if path_id == 0 {
		crl.dbi.AddPathByDomainId(path, domain_id)
		path_id = crl.dbi.GetPathId(domain_id, path)
	}

	for _, p := range params {
		crl.dbi.AddParamByPathId(p, p_type, path_id)
	}
}

func (crl *YCrawler) extractParams(parsed_link *url.URL) []string {
	m, _ := url.ParseQuery(parsed_link.RawQuery)
	r := []string{}
	for x, _ := range m {
		r = append(r, x)
	}
	return r
}

func InitCrawler(seed_url string, debug_level int, dbi *db.DbInstance) YCrawler {
	var baseURLRegexp = regexp.MustCompile(`^(https?:\/\/([a-zA-Z0-9_\.-]+))\/?.*$`)
	baseURL := baseURLRegexp.FindStringSubmatch(seed_url)[1]
	domain := baseURLRegexp.FindStringSubmatch(seed_url)[2]
	crl := YCrawler{
		&myQueue{[]string{}, sync.Mutex{}},
		debug_level,
		seed_url,
		domain,
		baseURL,
		map[string]int{},
		dbi}
	crl.queue.push(seed_url)
	return crl
}

func main() {
	max_procs := runtime.GOMAXPROCS(8)
	fmt.Println("GOMAXPROCS", max_procs)
	max_procs = runtime.GOMAXPROCS(8)
	fmt.Println("GOMAXPROCS", max_procs)

	//seed_url := "https://www.yahoo.com"
	//seed_url := "https://hulu.com"
	//heleo2

	seed_url := os.Args[1]

	mydb := db.DbInstance{DBPath: "../db/crawl.db"}
	mydb.GetDbInstance()
	defer mydb.CloseDB()

	crawler := InitCrawler(seed_url, 2, &mydb)
	crawler.Crawl()
}
