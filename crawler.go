package main

import (
	"crawler/db"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

type queueFrame struct {
	link  string
	depth int
}

type myQueue struct {
	q []queueFrame
	m sync.Mutex
}

func (q *myQueue) push(x string, d int) {
	(*q).m.Lock()
	(*q).q = append((*q).q, queueFrame{x, d})
	(*q).m.Unlock()
}

func (q *myQueue) pop() (string, int) {
	(*q).m.Lock()
	x := (*q).q[0].link
	d := (*q).q[0].depth
	if len((*q).q) == 1 {
		(*q).q = []queueFrame{}
	} else {
		(*q).q = (*q).q[1:]
	}
	(*q).m.Unlock()
	return x, d
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
	queue            *myQueue
	max_depth        int
	debug_level      int
	seed_url         string
	domain           string
	domain_id        int
	base_url         string
	visited          map[string]int
	dbi              *db.DbInstance
	log_file         string
	depth_cnt        map[int]int
	max_cnt_on_depth int
}

func (crl *YCrawler) Log(message string, level int, outFile string) {
	if level <= crl.debug_level {
		if outFile != "stdout" {
			f, err := os.OpenFile(outFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				panic(err)
			}
			defer f.Close()
			log.SetOutput(f)
			log.Println(message)
		} else {
			fmt.Println(message)
		}
	}
}

func (crl *YCrawler) normalizeURL(link, url string) string {
	// remove one trailing slash
	s_url := strings.Split(url, "/")
	if s_url[len(s_url)-1] == "" {
		url = url[:len(url)-1]
	}
	var normalized_url string = link
	if strings.HasPrefix(link, "//") {
		normalized_url = strings.Split(link, ":")[0] + "://" + link
	} else if strings.HasPrefix(link, "/") {
		normalized_url = crl.base_url + link
	} else if strings.HasPrefix(link, "http") {
		normalized_url = link
	} else {
		normalized_url = url + "/" + link
	}
	return strings.Split(normalized_url, "#")[0]
}

func (crl *YCrawler) Crawl() {
	crl.Log("crawl: running on "+crl.domain+", seed_url is "+crl.seed_url, 0, crl.log_file)
	for {
		if crl.queue.isEmpty() {
			crl.Log("crawl: The queue is empty!", 0, crl.log_file)
			return
		}
		url, depth := crl.queue.pop()
		if crl.max_depth > 0 && depth > crl.max_depth {
			continue
		}
		//fmt.Println("crawl: Popped ", url) //DEBUG
		if crl.debug_level > 10 {
			crl.queue.debug()
		}
		urlsch := make(chan string)
		go func() {
			//fmt.Println(url)
			crl.Fetch(url, urlsch, depth)
		}()

		func(c chan string) {
			for x := range c {
				crl.Log("crawl: Pushing url "+x+" Depth "+strconv.Itoa(depth+1), 7, crl.log_file)
				crl.queue.push(x, depth+1)
			}
		}(urlsch)
	}
}

func (crl *YCrawler) Fetch(url string, c chan string, depth int) {
	var error bool = false
	if _, ok := crl.visited[url]; ok {
		crl.Log("fetch: "+url+" visited", 2, crl.log_file)
		error = true
	}
	if crl.depth_cnt[depth] >= crl.max_cnt_on_depth {
		crl.Log("fetch: ("+url+") maximum cnt on depth "+strconv.Itoa(depth), 2, crl.log_file)
		error = true
	}
	if error {
		close(c)
		return
	}
	crl.Log("fetching "+url, 1, crl.log_file)
	crl.visited[url] = 1
	crl.depth_cnt[depth] += 1
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
		crl.Log("Cannot fetch url "+lnk+": "+err.Error(), 2, crl.log_file)
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

			if crl.checkRestrictions(normalized_url) {
				u, err := url.Parse(normalized_url)
				if err != nil {
					panic(err)
				}

				get_params := crl.extractParams(u)
				crl.addParamsToDB(get_params, u.Path, "GET")

				if form_found {
					crl.Log("The form action = "+link+" method "+form_method+", enctype "+form_enctype+" found", 1, crl.log_file)
					crl.addParamsToDB(post_params, u.Path, form_method)
				}

				crl.Log("\t--> "+normalized_url, 3, crl.log_file)
				urls = append(urls, normalized_url)
			}
		})
	return urls
}

func (crl *YCrawler) checkRestrictions(link string) bool {
	if !crl.isSameDomain(link) {
		crl.Log("Same host restriction for foreign url "+link, 3, crl.log_file)
		return false
	}
	if crl.isStaticURL(link) {
		crl.Log("Static content restriction "+link, 3, crl.log_file)
		return false
	}
	return true
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
	path_id := crl.dbi.GetPathId(crl.domain_id, path)

	if path_id == 0 {
		crl.dbi.AddPathByDomainId(path, crl.domain_id)
		path_id = crl.dbi.GetPathId(crl.domain_id, path)
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

func InitCrawler(seed_url, log_file string, max_depth int, debug_level int, dbi *db.DbInstance, max_cnt_on_depth int) YCrawler {
	var baseURLRegexp = regexp.MustCompile(`^(https?:\/\/([a-zA-Z0-9_\.-]+))\/?.*$`)
	baseURL := baseURLRegexp.FindStringSubmatch(seed_url)[1]
	domain := baseURLRegexp.FindStringSubmatch(seed_url)[2]
	domain_id := dbi.GetDomainId(domain)
	if domain_id == 0 {
		dbi.AddDomain(domain)
		domain_id = dbi.GetDomainId(domain)
	}
	depth_cnt := map[int]int{}

	crl := YCrawler{
		&myQueue{[]queueFrame{}, sync.Mutex{}},
		max_depth,
		debug_level,
		seed_url,
		domain,
		domain_id,
		baseURL,
		map[string]int{},
		dbi,
		log_file,
		depth_cnt,
		max_cnt_on_depth}
	crl.queue.push(seed_url, 0)
	return crl
}

/*  Debug levels:
*   0 - show always, critical messages
*   1 - info about url currently fetching
*   2 - fetching debug (e.g. visited urls, found forms and so on)
*   3 - debugging info about all links on the page
*   7 - debugging info about pushing to the queue
*   10 - debug queue
 */

// Seed URL, depth, and log_level can be passed in args in this order
// These parameters can be also set in the crawler.conf file
// Also in that file we can set max_procs, max_depth, db_engine, DB
func main() {
	configFile, e := ioutil.ReadFile("./crawler.conf")
	if e != nil {
		panic(e)
	}
	var configMap map[string]string
	json.Unmarshal(configFile, &configMap)

	if len(os.Args) < 2 {
		fmt.Println("Usage: " + os.Args[0] + " URL [depth] [log_level]")
		os.Exit(1)
	}

	if len(os.Args) > 2 {
		configMap["depth"] = os.Args[2]
	} else {
		configMap["depth"] = configMap["max_depth"]
	}
	if len(os.Args) > 3 {
		configMap["log_level"] = os.Args[3]
	} else {
		configMap["log_level"] = "0"
	}

	//for k, v := range configMap {
	//	fmt.Println(k, " => ", v)
	//}

	max_procs, err := strconv.Atoi(configMap["max_procs"])
	if err != nil {
		panic(err)
	}
	runtime.GOMAXPROCS(max_procs)

	seed_url := os.Args[1]

	max_depth, err := strconv.Atoi(configMap["depth"])
	if err != nil {
		panic(err)
	}

	log_level, err := strconv.Atoi(configMap["log_level"])
	if err != nil {
		panic(err)
	}

	var max_cnt_on_depth int
	max_cnt_on_depth, err = strconv.Atoi(configMap["max_cnt_on_depth"])
	if err != nil {
		max_cnt_on_depth = 1000
	}
	fmt.Println("max_cnt_on_depth ", max_cnt_on_depth) //DEBUG

	sep := string(os.PathSeparator)
	sqlite_db_path := os.Getenv("GOPATH") + sep + "db" + sep + "crawl.db"
	fmt.Println("db_path: ", sqlite_db_path)

	//mydb := db.SQLiteInstance{DBPath: sqlite_db_path}
	mydb := db.DbInstance{DbEngine: configMap["db_engine"],
		ConnectionString: configMap["db_connection_string"]}
	mydb.GetDbInstance()
	defer mydb.CloseDB()

	if len(configMap["log_file"]) == 0 {
		configMap["log_file"] = "stdout"
	}

	crawler := InitCrawler(seed_url, configMap["log_file"], max_depth, log_level, &mydb, max_cnt_on_depth)
	crawler.Crawl()
}
