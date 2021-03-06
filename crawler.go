package main

//TODO: add 3xx to database in collectUrls
//TODO remove prints
//TODO work with scope

import (
	"crawler/db"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
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
	project_id       int
	headers          []string
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
	// cut off the last slash with the filename
	s_url := strings.Split(url, "/")
	url = strings.Join(s_url[:len(s_url)-1], "/")
	var normalized_url string = link
	// Two leading slashes says that we should use the same scheme as for base URL
	if strings.HasPrefix(link, "//") {
		normalized_url = strings.Split(url, ":")[0] + ":" + link
	} else if strings.HasPrefix(link, "/") {
		// currently we are fobridden to move to another domain, so
		// it cannot change while we are crawling
		normalized_url = crl.base_url + link
	} else if strings.HasPrefix(link, "http") {
		//TODO: maybe add other schemes????
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
	/*
		if _, ok := crl.visited[url]; ok {
			crl.Log("fetch: "+url+" visited", 2, crl.log_file)
			error = true
		}
	*/
	if crl.depth_cnt[depth] >= crl.max_cnt_on_depth {
		crl.Log("fetch: ("+url+") maximum cnt on depth "+strconv.Itoa(depth), 2, crl.log_file)
		error = true
	}
	if error {
		close(c)
		return
	}

	//crl.visited[url] = 1
	crl.depth_cnt[depth] += 1
	urls := crl.collectUrls(url)
	for _, s := range urls {
		c <- s
	}
	close(c)
	return
}

func (crl *YCrawler) collectUrls(lnk string) []string {
	if _, ok := crl.visited[lnk]; ok {
		crl.Log("collectUrls: "+lnk+" visited", 2, crl.log_file)
		return []string{}
	}

	crl.Log("collectUrls: fetching "+lnk, 1, crl.log_file)

	httpClient := &http.Client{}
	req, _ := http.NewRequest("GET", lnk, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:58.0) Gecko/20100101 Firefox/58.0")

	var logoutRegexp = regexp.MustCompile(`(.*logout.*)|(.*logoff.*)`)
	if !logoutRegexp.MatchString(lnk) {
		for _, header := range crl.headers {
			harr := strings.SplitN(header, ":", 2)
			if len(harr) > 1 {
				//fmt.Println("Setting header " + strings.Trim(harr[0], " ") + " " + strings.Trim(harr[1], " "))
				req.Header.Set(strings.Trim(harr[0], " "), strings.Trim(harr[1], " "))
			}
		}
	}

	//resp, err := http.Get(lnk)
	resp, err := httpClient.Do(req)
	if err != nil {
		return []string{}
	}

	if resp.Request.URL.Hostname() != crl.domain {
		crl.Log("Restricting redirect to foreign domain "+resp.Request.URL.Hostname(), 2, crl.log_file)
		return []string{}
	}

	baseURL := lnk
	currentURL, _ := url.Parse(baseURL)
	if resp.Request.URL.RequestURI() != currentURL.RequestURI() {
		/*	baseURL = resp.Request.URL.Scheme + "://" +
			resp.Request.URL.Hostname() +
			resp.Request.URL.RequestURI()
		*/
		baseURL = resp.Request.URL.String()
	}

	crl.visited[baseURL] = 1

	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		crl.Log("Cannot fetch url "+baseURL+": "+err.Error(), 2, crl.log_file)
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

			check_attrs := []string{"href", "src"}
			post_params := [][]string{}

			// We want to check HTML tags only
			if len(item.Nodes) > 0 && item.Nodes[0].Type == html.ElementNode {
				// At first, we'll check the "form" tag
				// If we found it we parse parameters
				// We don't want to send any more requests to this page in a way
				// to parse form inputs, so it should be done now.
				if item.Nodes[0].Data == "form" {
					form_found = true
					form_method, _ = item.Attr("method")
					form_enctype, _ = item.Attr("enctype")
					link, _ = item.Attr("action")
					item.Find("input").Each(func(i int, x *goquery.Selection) {
						input_name, _ := x.Attr("name")
						value, _ := x.Attr("value")
						//input_type, _ := x.Attr("type")
						post_params = append(post_params, []string{input_name, value})
					})
					if len(link) == 0 {
						link = baseURL
					}
				} else {
					// For each HTML element check attributes which can contain an URL
					for _, tag := range check_attrs {
						if link, ok = item.Attr(tag); ok {
							break
						}
					}
				}
			}
			/*
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
								value, _ := x.Attr("value")
								//input_type, _ := x.Attr("type")
								post_params = append(post_params, []string{input_name, value})
							})
						}
						break
					}
				}
			*/

			// Element has no interesting attributes or they are empty.
			if len(link) == 0 {
				return
			}

			// Not-http link.
			linkSplitted := strings.Split(link, ":")
			if len(linkSplitted) > 1 {
				scheme := linkSplitted[0]
				if scheme != "http" && scheme != "https" {
					crl.Log("Non-http link"+link, 10, crl.log_file)
					return
				}
			}

			normalized_url := crl.normalizeURL(link, baseURL)

			if crl.checkRestrictions(normalized_url) {
				u, err := url.Parse(normalized_url)
				if err != nil {
					panic(err)
				}

				get_params := crl.extractParams(u)
				crl.addParamsToDB(get_params, u.Path, "GET", u.Scheme)

				if form_found == true {
					crl.Log("The form action = "+link+" method "+form_method+", enctype "+form_enctype+" found", 1, crl.log_file)
					crl.addParamsToDB(post_params, u.Path, form_method, u.Scheme)
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
	var rxStatic = regexp.MustCompile(`(.*\.zip)|(.*\.js)|(.*\.css)|(.*\.pdf)|(.*\.woff)|(.*\.jpg)|(.*\.jpeg)|(.*\.doc.*)|(.*\.gif)|(.*\.png)|(.*\.avi)|(.*\.mpeg.*)|(.*\.mpg.*)|(.*\.ppt.*)|(.*\.xls.*)|(.*\.mp4.*)|(.*\.exe.*)|(.*\.apk)`)
	return rxStatic.MatchString(link)
}

func (crl *YCrawler) addParamsToDB(params [][]string, path, p_type, scheme string) {
	if len(params) == 0 {
		return
	}
	path_id := crl.dbi.GetPathId(crl.domain_id, path)

	if path_id == 0 {
		crl.dbi.AddPathByDomainId(path, crl.domain_id, scheme)
		path_id = crl.dbi.GetPathId(crl.domain_id, path)
	}

	for _, p := range params {
		crl.dbi.AddParamByPathId(p[0], p[1], p_type, path_id)
	}
}

func (crl *YCrawler) extractParams(parsed_link *url.URL) [][]string {
	m, _ := url.ParseQuery(parsed_link.RawQuery)
	r := [][]string{}
	for k, v := range m {
		r = append(r, []string{k, v[0]})
	}
	return r
}

func InitCrawler(
	seed_url, log_file string,
	max_depth, debug_level, max_cnt_on_depth, project_id int,
	dbi *db.DbInstance) YCrawler {
	var baseURLRegexp = regexp.MustCompile(`^(https?:\/\/([a-zA-Z0-9_\.-]+))\/?.*$`)
	baseURL := baseURLRegexp.FindStringSubmatch(seed_url)[1]
	domain := baseURLRegexp.FindStringSubmatch(seed_url)[2]
	domain_id := dbi.GetDomainId(domain, project_id)
	//fmt.Println("My domain ID is ", domain_id)
	if domain_id == 0 {
		if !dbi.CheckProjectId(project_id) {
			log.Println("Can't init crawler: no projects with id ", project_id)
			os.Exit(1)
		}
		dbi.AddDomain(domain, project_id)
		domain_id = dbi.GetDomainId(domain, project_id)
	}
	depth_cnt := map[int]int{}
	headers := dbi.GetHeaders(project_id)

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
		max_cnt_on_depth,
		project_id,
		headers}
	crl.queue.push(seed_url, 0)
	return crl
}

func usage() {
	fmt.Println("Usage: " + os.Args[0] + " URL [depth] [log_level] [config_file] [project_id]")
}

func parseConfig(config_file string) (map[string]string, bool) {
	f, e := ioutil.ReadFile(config_file)
	if e != nil {
		return map[string]string{}, true
	}
	var configMap map[string]string = map[string]string{}
	json.Unmarshal(f, &configMap)
	return configMap, false
}

func parseArgs(args []string) (map[string]string, bool) {
	cl_arg_names := []string{"url", "depth", "log_level", "config_file", "project_id"}
	var configMap map[string]string = map[string]string{}
	for i := 0; i < min(len(cl_arg_names), len(args)-1); i++ {
		//fmt.Println("arg " + cl_arg_names[i] + " found, value is " + args[i+1])
		configMap[cl_arg_names[i]] = args[i+1]
	}

	config_file := "./crawler.conf"
	if val, ok := configMap["config_file"]; ok {
		config_file = val
	}

	configMap2, err := parseConfig(config_file)
	if err {
		fmt.Println("Can't open config file \"" + config_file + "\"")
	}

	for k, v := range configMap2 {
		if _, ok := configMap[k]; !ok {
			configMap[k] = v
		}
	}
	if _, ok := configMap["url"]; !ok {
		fmt.Println("Pass the 'url' parameter in the argument")
		return nil, true
	}
	if _, ok := configMap["log_file"]; !ok {
		configMap["log_file"] = "stdout"
	}
	return configMap, false
}

func validateNumericalArgs(configMap map[string]string) (map[string]int, bool) {
	numeric_args := []string{"depth", "log_level", "project_id", "max_procs", "max_cnt_on_depth", "max_depth"}
	var configMapNum map[string]int = map[string]int{}
	for _, x := range numeric_args {
		if val, ok := configMap[x]; ok {
			intVal, err := strconv.Atoi(val)
			if err != nil {
				fmt.Println("validateNumericalArgs: param " + x + " must be integer")
				return nil, true
			}
			configMapNum[x] = intVal
		}
	}
	if _, ok := configMapNum["log_level"]; !ok {
		configMapNum["log_level"] = 0
	}
	if _, ok := configMapNum["max_cnt_on_depth"]; !ok {
		configMapNum["max_cnt_on_depth"] = 1000
	}
	if _, ok := configMapNum["project_id"]; !ok {
		configMapNum["project_id"] = 1
	}
	if _, ok := configMapNum["depth"]; !ok {
		fmt.Println("Set the 'max_depth' parameter in the config or pass it in the argument")
		return nil, true
	}
	return configMapNum, false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	configMap, err1 := parseArgs(os.Args)
	if err1 {
		fmt.Println("Can't parse args")
		usage()
		os.Exit(1)
	}
	configMapInt, err2 := validateNumericalArgs(configMap)
	if err2 {
		fmt.Println("Validation errors")
		usage()
		os.Exit(1)
	}

	/*
		fmt.Println("Config Map")
		for k, v := range configMap {
			fmt.Println(k, " => ", v)
		}
		fmt.Println("Config Map Int")
		for k, v := range configMapInt {
			fmt.Println(k, " => ", v)
		}
	*/

	runtime.GOMAXPROCS(configMapInt["max_procs"])

	mydb := db.DbInstance{DbEngine: configMap["db_engine"],
		ConnectionString: configMap["db_connection_string"]}
	mydb.GetDbInstance()
	defer mydb.CloseDB()

	//proxyUrl, _ := url.Parse("https://localhost:8080")
	//http.DefaultTransport = &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	timeout := time.Duration(5 * time.Second)

	httpClient := &http.Client{Timeout: timeout}
	req, _ := http.NewRequest("GET", configMap["url"], nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:58.0) Gecko/20100101 Firefox/58.0")

	//resp, err := http.Get(configMap["url"])
	resp, err := httpClient.Do(req)
	if err != nil {
		panic(err)
	}
	//fmt.Print(resp.Request.URL.Scheme, "://", resp.Request.URL.Hostname())
	actualDomainArray := strings.Split(resp.Request.URL.Hostname(), ".")
	if (len(actualDomainArray)) < 2 {
		panic("main: Invalid url, exiting!")
	}
	actualDomain := strings.TrimSpace(strings.Join(actualDomainArray[len(actualDomainArray)-2:], "."))
	if !strings.Contains(configMap["url"], actualDomain) {
		fmt.Println("Domain " + actualDomain + " not in scope")
		return
	}

	crawler := InitCrawler(
		configMap["url"],
		configMap["log_file"],
		configMapInt["max_depth"],
		configMapInt["log_level"],
		configMapInt["max_cnt_on_depth"],
		configMapInt["project_id"],
		&mydb)
	crawler.Crawl()
}
