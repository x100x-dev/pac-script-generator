package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

//var ifForced = flag.Bool("force", false, "If to ignore checking of an updated dump.csv available")
var ifForced = flag.Bool("force", true, "If to ignore checking of an updated dump.csv available")

type blockProvider struct {
	urls []string
}

var blockProviders = []blockProvider{
	{
		urls: []string{
			"https://svn.code.sf.net/p/zapret-info/code/dump.csv",
		},
	},
}

var get = func(url string) (*http.Response, error) {

	fmt.Println("GETting " + url)

	response, err := http.Get(url)

	fmt.Println("Got")

	if err != nil {
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		return response, fmt.Errorf(
			"Negative status code: %d. For url: %s",
			response.StatusCode,
			url,
		)
	}

	return response, nil
}

var getOrDie = func(url string) *http.Response {

	response, err := get(url)

	if err != nil {
		panic(err)
	}

	return response
}

type GhCommit struct {
	Message string `json:"message,omitempty"`
	Tree    string `json:"tree,omitempty"`
}

func main() {

	GH_REPO := os.Getenv("GH_REPO")
	GH_TOKEN := os.Getenv("GH_TOKEN")

	if GH_REPO == "" || GH_TOKEN == "" {
		panic("Provide GH_REPO and GH_TOKEN environment variables!")
	}

	REPO_URL := "https://api.github.com/repos/" + GH_REPO

	var (
		text     []byte
		response *http.Response
		err      error
	)

	HOSTNAMES := make(map[string]bool)

	flag.Parse()

	bestProvider := &blockProviders[0]

	urls := bestProvider.urls

	fmt.Println("Best provider urls are:", urls)

	// Ignored hostnames

	response = getOrDie("https://bitbucket.org/ValdikSS/antizapret/raw/master/ignorehosts.txt")

	fmt.Println("Downloaded ignoredhosts.")

	ignoredHostnames := map[string]bool{
		"ynet.co.il":         true,
		"www.ynet.co.il":     true,
		"rutrk.org":          true,
		"pornhub.com":        true,
		"www.pornhub.com":    true,
		"rt.pornhub.com":     true,
		"ru.pornhub.com":     true,
		"ytimg.com":          true,
		"i.ytimg.com":        true,
		"tiktok.com":         true,
		"ggpht.com":          true,
		"akamaihd.net":       true,
		"youtube.com":        true,
		"www.youtube.com":    true,
		"googlevideo.com":    true,
		".googlevideo.com":   true,
		"prostovpn.org":      true,
		"antizapret.prostovpn.org": true,
	}

	scanner := bufio.NewScanner(response.Body)

	for scanner.Scan() {
		ignoredHostnames[scanner.Text()] = true
	}

	response.Body.Close()

	fmt.Println("Parsed ignoredhosts.txt.")

	// NX domains

	response = getOrDie("https://raw.githubusercontent.com/zapret-info/z-i/master/nxdomain.txt")

	fmt.Println("Downloaded nxdomains.")

	nxdomains := make(map[string]bool)

	scanner = bufio.NewScanner(response.Body)

	for scanner.Scan() {
		nxdomains[scanner.Text()] = true
	}

	if err := scanner.Err(); err != nil {
		panic(err)
	}

	response.Body.Close()

	fmt.Println("Parsed nxdomains.")

	// TSPU list

	response = getOrDie("https://raw.githubusercontent.com/x100x-dev/TSPU/main/domains.json")

	text, err = ioutil.ReadAll(response.Body)

	if err != nil {
		panic(err)
	}

	response.Body.Close()

	tspus := &[]struct {
		Domains []string
	}{}

	json.Unmarshal(text, tspus)

	for _, record := range (*tspus) {
		for _, hostname := range record.Domains {
			HOSTNAMES[hostname] = true
		}
	}

	fmt.Println("Got shaped hostnames (TSPU).")

	var lastError error

	for _, url := range urls {

		response, err = get(url)

		if err == nil {
			break
		}

		lastError = err
		response = nil
	}

	if response == nil {
		panic(lastError)
	}

	csvIn := bufio.NewReader(response.Body)

	fmt.Println("Downloaded csv.")

	_, err = csvIn.ReadString('\n')

	if err != nil {
		panic(err)
	}

	reader := csv.NewReader(
		transform.NewReader(csvIn, charmap.Windows1251.NewDecoder()),
	)

	reader.Comma = ';'
	reader.FieldsPerRecord = -1

	idnaProfile := idna.New()

	customHostnames := map[string]bool{
		"archive.org": true,
		"bitcoin.org": true,
		"linkedin.com": true,
		"licdn.com": true,
		"claude.ai": true,
		"recraft.ai": true,
		"meta.ai": true,
		"grok.com": true,
		"x.ai": true,
		"figma.com": true,
		"telegram.org": true,
		"t.me": true,
		"mega.nz": true,
		"notion.so": true,
		"deepl.com": true,
	}

	for hostname, ifBlocked := range customHostnames {
		HOSTNAMES[hostname] = ifBlocked
	}

	customHostnames = nil

	runtime.GC()

	ipv4 := make(map[string]bool)
	ipv4subnets := make(map[string]bool)
	ipv6 := make(map[string]bool)

	for {

		record, err := reader.Read()

		if err != nil {

			if err == io.EOF {
				break
			}

			panic(err)
		}

		ifHasHostname := len(record) > 1

		hostnamesSlice := strings.Split(record[1], "|")

		for _, hostname := range hostnamesSlice {

			hostname = strings.Trim(hostname, " \t")

			if hostname != "" {

				hostname, err := idnaProfile.ToASCII(hostname)

				if err != nil {
					panic(err)
				}

				if strings.HasPrefix(hostname, "*.") {
					hostname = hostname[2:]
				}

				if nxdomains[hostname] || ignoredHostnames[hostname] {
					continue
				}

				if strings.HasPrefix(hostname, "www.") {
					hostname = hostname[4:]
				}

				HOSTNAMES[hostname] = true
				ifHasHostname = true
			}
		}

		if !ifHasHostname {

			ips := strings.Split(record[0], "|")

			for _, ip := range ips {

				ip = strings.Trim(ip, " \t")

				ifIpV6 := strings.ContainsAny(ip, ":")

				if ifIpV6 {
					ipv6[ip] = true
					continue
				}

				ifSubnet := strings.ContainsAny(ip, "/")

				if ifSubnet {
					ipv4subnets[ip] = true
					continue
				}

				ipv4[ip] = true
			}
		}
	}

	response.Body.Close()

	fmt.Println("Parsed csv.")

	runtime.GC()

	// Additional hostname source

	response = getOrDie(
		"https://raw.githubusercontent.com/bol-van/rulist/refs/heads/main/reestr_hostname_resolvable_ip4.txt",
	)

	fmt.Println("Downloaded additional hostname list.")

	scanner = bufio.NewScanner(response.Body)

	for scanner.Scan() {

		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)

		for _, field := range fields {

			hostname := strings.Trim(field, " \t")

			if net.ParseIP(hostname) != nil {
				continue
			}

			hostname = strings.TrimPrefix(hostname, "*.")
			hostname = strings.TrimPrefix(hostname, "www.")

			hostname, err = idnaProfile.ToASCII(hostname)

			if err != nil {
				continue
			}

			if nxdomains[hostname] || ignoredHostnames[hostname] {
				continue
			}

			HOSTNAMES[hostname] = true
		}
	}

	if err := scanner.Err(); err != nil {
		panic(err)
	}

	response.Body.Close()

	fmt.Println("Parsed additional hostname list.")

	// Converts IP mask to int

	addrToInt := func(in []byte) int {

		var i int32

		buf := bytes.NewReader(in)

		err := binary.Read(buf, binary.BigEndian, &i)

		if err != nil {
			panic(err)
		}

		return int(i)
	}

	getSubnets := func(m map[string]bool) [][]int {

		keys := make([][]int, len(m))

		i := 0

		for maskedNet := range m {

			_, mask, err := net.ParseCIDR(maskedNet)

			if err != nil {
				panic(err)
			}

			keys[i] = []int{
				addrToInt([]byte(mask.IP)),
				addrToInt([]byte(mask.Mask)),
			}

			i++
		}

		return keys
	}

	getOptimizedMap := func(m map[string]bool) map[int]string {

		opt := make(map[int][]string)

		for key := range m {

			length := len(key)

			if opt[length] == nil {
				opt[length] = []string{key}
				continue
			}

			opt[length] = append(opt[length], key)
		}

		opt2 := make(map[int]string)

		for key := range opt {

			sort.Strings(opt[key])

			opt2[key] = strings.Join(opt[key], "")
		}

		return opt2
	}

	ipv4Map := getOptimizedMap(ipv4)
	ipv4subnetsKeys := getSubnets(ipv4subnets)
	hostnamesMap := getOptimizedMap(HOSTNAMES)

	ipv4 = nil
	ipv6 = nil
	ipv4subnets = nil
	HOSTNAMES = nil

	runtime.GC()

	fmt.Println("Opening template...")

	tmpl, err := template.ParseFiles("./template.js")

	if err != nil {
		panic(err)
	}

	values := &struct {
		IPS            map[int]string
		HOSTNAMES      map[int]string
		MASKED_SUBNETS [][]int
	}{
		IPS:            ipv4Map,
		HOSTNAMES:      hostnamesMap,
		MASKED_SUBNETS: ipv4subnetsKeys,
	}

	marshalled, err := json.Marshal(values)

	if err != nil {
		panic(err)
	}

	builder := new(strings.Builder)

	fmt.Println("Rendering template...")

	err = tmpl.ExecuteTemplate(
		builder,
		"template.js",
		struct{ INPUTS string }{
			INPUTS: string(marshalled),
		},
	)

	if err != nil {
		panic(err)
	}

	generatedPAC := builder.String()

	

	fmt.Println("Getting current PAC...")

	response = getOrDie(REPO_URL + "/contents/anticensority.pac")

	text, err = ioutil.ReadAll(response.Body)

	if err != nil {
		panic(err)
	}

	response.Body.Close()

	currentPac := &struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}{}

	json.Unmarshal(text, currentPac)

	oldHash := ""

	if currentPac.Content != "" {

	decoded := strings.ReplaceAll(currentPac.Content, "\n", "")

	oldContent, err := io.ReadAll(
		base64.NewDecoder(
			base64.StdEncoding,
			strings.NewReader(decoded),
		),
	)

	if err != nil {
		panic(err)
	}

	oldPac := string(oldContent)

	// normalize line endings
	oldPac = strings.ReplaceAll(oldPac, "\r\n", "\n")
	oldPac = strings.ReplaceAll(oldPac, "\r", "\n")

	// remove Updated line
	lines := strings.Split(oldPac, "\n")

	if len(lines) > 0 && strings.HasPrefix(lines[0], "// Updated:") {
		oldPac = strings.Join(lines[1:], "\n")
	}

	// normalize trailing spaces/newlines
	oldPac = strings.TrimSpace(oldPac)

	newGeneratedPac := strings.ReplaceAll(generatedPAC, "\r\n", "\n")
	newGeneratedPac = strings.ReplaceAll(newGeneratedPac, "\r", "\n")
	newGeneratedPac = strings.TrimSpace(newGeneratedPac)

	oldHashBytes := sha256.Sum256([]byte(oldPac))
	oldHash = hex.EncodeToString(oldHashBytes[:])

	newHashBytes := sha256.Sum256([]byte(newGeneratedPac))
	newHash = hex.EncodeToString(newHashBytes[:])
}
	if oldHash == newHash {

		fmt.Println("PAC unchanged. Exiting.")

		os.Exit(0)
	}

	newUpdateMessage := "Updated: " +
		time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

	finalPac := "// " + newUpdateMessage + "\n" + generatedPAC

	fmt.Println("PAC changed.")

	marshalled = nil
	values = nil
	ipv4Map = nil
	hostnamesMap = nil
	ipv4subnetsKeys = nil

	runtime.GC()

	fmt.Println("Getting README...")

	response = getOrDie(REPO_URL + "/readme/")

	text, err = ioutil.ReadAll(response.Body)

	if err != nil {
		panic(err)
	}

	response.Body.Close()

	readme := &struct {
		Sha  string
		Path string
	}{}

	json.Unmarshal(text, readme)

	type gitFile struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		Content string `json:"content,omitempty"`
		Sha     string `json:"sha,omitempty"`
	}

	body := &struct {
		Tree []gitFile `json:"tree"`
	}{
		Tree: make([]gitFile, 2),
	}

	body.Tree[0] = gitFile{
		Path:    "anticensority.pac",
		Mode:    "100644",
		Type:    "blob",
		Content: finalPac,
	}

	body.Tree[1] = gitFile{
		Path: readme.Path,
		Mode: "100644",
		Type: "blob",
		Sha:  readme.Sha,
	}

	marshalled, err = json.Marshal(body)

	if err != nil {
		panic(err)
	}

	runtime.GC()

	doOrDie := func(method, url string, payload []byte) *http.Response {

		fmt.Println(method+"ing to", url)

		req, err := http.NewRequest(
			method,
			url,
			bytes.NewReader(payload),
		)

		if err != nil {
			panic(err)
		}

		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+GH_TOKEN)

		response, err = http.DefaultClient.Do(req)

		if err != nil {
			panic(err)
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {

			fmt.Println(
				"Negative status code: " +
					strconv.Itoa(response.StatusCode) +
					". For url: " + url,
			)

			panic(method + " failed.")
		}

		fmt.Println(method + "ed.")

		return response
	}

	response = doOrDie(
		"POST",
		REPO_URL+"/git/trees",
		marshalled,
	)

	text, err = ioutil.ReadAll(response.Body)

	if err != nil {
		panic(err)
	}

	response.Body.Close()

	tree := &struct {
		Sha string
	}{}

	json.Unmarshal(text, tree)

	commit := &GhCommit{
		Message: newUpdateMessage,
		Tree:    tree.Sha,
	}

	marshalled, err = json.Marshal(commit)

	if err != nil {
		panic(err)
	}

	response = doOrDie(
		"POST",
		REPO_URL+"/git/commits",
		marshalled,
	)

	text, err = ioutil.ReadAll(response.Body)

	if err != nil {
		panic(err)
	}

	response.Body.Close()

	patch := &struct {
		Sha   string `json:"sha"`
		Force bool   `json:"force,omitempty"`
	}{}

	json.Unmarshal(text, patch)

	patch.Force = true

	marshalled, err = json.Marshal(patch)

	if err != nil {
		panic(err)
	}

	response = doOrDie(
		"PATCH",
		REPO_URL+"/git/refs/heads/master",
		marshalled,
	)

	response.Body.Close()

	fmt.Println("Done.")
}
