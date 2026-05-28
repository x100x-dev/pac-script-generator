package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/csv"
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

	"golang.org/x/net/idna"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// Измените на false, если хотите, чтобы проверка обновлений работала по умолчанию
var ifForced = flag.Bool("force", true, "If to ignore checking of an updated dump.csv available")

var get = func(url string) (*http.Response, error) {
	fmt.Println("GETting " + url)
	response, err := http.Get(url)
	fmt.Println("Got")
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return response, fmt.Errorf("Negative status code: " + strconv.Itoa(response.StatusCode) + ". For url: " + url)
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

type GhCommits []struct {
	Commit GhCommit
}

type GhCommitInfo []struct {
	Commit struct {
		Committer struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
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
	lastUpdateMessage := ""
	flag.Parse()

	if *ifForced == false {
		response := getOrDie(REPO_URL + "/commits")
		text, err = ioutil.ReadAll(response.Body)
		if err != nil {
			panic(err)
		}
		response.Body.Close()
		commits := &GhCommits{}
		json.Unmarshal(text, commits)
		if len(*commits) > 0 {
			lastUpdateMessage = (*commits)[0].Commit.Message
		}
	}

	var newUpdateMessage string

	// Проверяем дату обновления целевого файла reestr_hostname_resolvable_ip4.txt
	fmt.Println("Checking updates for reestr_hostname_resolvable_ip4.txt...")
	reqUrl := "https://api.github.com/repos/bol-van/rulist/commits?path=reestr_hostname_resolvable_ip4.txt&per_page=1"
	
	req, err := http.NewRequest("GET", reqUrl, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+GH_TOKEN)
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Failed to get file commits: %s", resp.Status))
	}
	
	text, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		panic(err)
	}

	fileCommits := &GhCommitInfo{}
	if err := json.Unmarshal(text, fileCommits); err != nil {
		panic(err)
	}

	if len(*fileCommits) == 0 {
		panic("No commits found for reestr_hostname_resolvable_ip4.txt")
	}

	// Формируем метку времени обновления
	newUpdateMessage = "Updated: " + (*fileCommits)[0].Commit.Committer.Date

	if *ifForced == false {
		if lastUpdateMessage >= newUpdateMessage {
			fmt.Println("No newer reestr_hostname_resolvable_ip4.txt published yet!")
			os.Exit(0)
		}
	}
	fmt.Println("New update message will be:", newUpdateMessage)

	// Игнорируемые хосты
	response = getOrDie("https://bitbucket.org/ValdikSS/antizapret/raw/master/ignorehosts.txt")
	fmt.Println("Downloaded ingoredhosts.")

	ignoredHostnames := map[string]bool{
		"ynet.co.il":                                         true,
		"www.ynet.co.il":                                     true,
		"rutrk.org":                                          true,
		"pornhub.com":                                        true,
		"www.pornhub.com":                                    true,
		"rt.pornhub.com":                                     true,
		"ru.pornhub.com":                                     true,
		"ytimg.com":                                          true,
		"i.ytimg.com":                                        true,
		"tiktok.com":                                         true,
		"ggpht.com":                                          true,
		"akamaihd.net":                                       true,
		"youtube.com":                                        true,
		"www.youtube.com":                                    true,
		"googlevideo.com":                                    true,
		".googlevideo.com":                                   true,
		"prostovpn.org":                                      true,
		"antizapret.prostovpn.org":                           true,
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		ignoredHostnames[scanner.Text()] = true
	}
	response.Body.Close()
	fmt.Println("Parsed ingoredhosts.txt.")

	// Несуществующие домены
	response = getOrDie("https://raw.githubusercontent.com/zapret-info/z-i/master/nxdomain.txt")
	fmt.Println("Downloaded nxdomians.")

	nxdomains := make(map[string]bool)
	scanner = bufio.NewScanner(response.Body)
	for scanner.Scan() {
		nxdomains[scanner.Text()] = true
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	response.Body.Close()
	fmt.Println("Parsed nxdomians.")

	// ТСПУ (TSPU)
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
	for _, record := range *tspus {
		for _, hostname := range record.Domains {
			HOSTNAMES[hostname] = true
		}
	}
	fmt.Println("Got shaped hostnames (TSPU).")

	// Прямое скачивание dump.csv
	csvUrl := "https://svn.code.sf.net/p/zapret-info/code/dump.csv"
	response, err = get(csvUrl)
	if err != nil {
		panic(err)
	}
	
	csvIn := bufio.NewReader(response.Body)
	fmt.Println("Downloaded csv.")

	_, err = csvIn.ReadString('\n')
	if err != nil {
		panic(err)
	}

	reader := csv.NewReader(transform.NewReader(csvIn, charmap.Windows1251.NewDecoder()))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	idnaAnalyse := idna.New()
	
	customHostnames := map[string]bool{
		"ua": true, "pravdabeslana.ru": true, "putinism.wordpress.com": true,
		"6090m01.wordpress.com": true, "archive.org": true, "bitcoin.org": true,
		"licdn.com": true, "linkedin.com": true, "koshara.net": true, "koshara.co": true,
		"new-team.org": true, "fast-torrent.ru": true, "pornreactor.cc": true,
		"vn.reactor.cc": true, "vatnik.reactor.cc": true, "nnm-club.name": true,
		"rutor.info": true, "free-rutor.org": true, "doramacine.in": true,
		"static.t-ru.org": true, "nnm-club.ws": true, "lostfilm.tv": true,
		"e-hentai.org": true, "kaztorka.org": true, "familysearch.org": true,
		"fastproxy.online": true, "dlinkddns.com": true, "padlet.com": true,
		"ionos.com": true, "alphacoders.com": true, "claude.ai": true, "reka.ai": true,
		"timehd.club": true, "rebrand.ly": true, "skycdp.com": true, "wildstat.com": true,
		"tria.ge": true, "rulate.ru": true, "fanficus.com": true,
		"fanficus-server-mirror-879c30cd977f.herokuapp.com": true, "apollo.farm": true,
		"station.money": true, "skip.build": true, "iar.com": true, "st.com": true,
		"howlongtobeat.com": true, "lingq.com": true, "thealphacentauri.net": true,
		"notion.so": true, "clickup.com": true, "recraft.ai": true, "kinoplay2.site": true,
		"gethomebank.org": true, "themoviedb.org": true, "optimism.io": true,
		"documentingreality.com": true, "coda.io": true, "puffer.fi": true,
		"kamino.finance": true, "debridge.foundation": true, "zora.co": true,
		"govgen.io": true, "saga.xyz": true, "grassfoundation.io": true, "getgrass.io": true,
		"orbit-games.com": true, "hetzner.com": true, "audiobookbay.lu": true,
		"humblebundle.com": true, "capacitorjs.com": true, "blur.io": true,
		"notepad-plus-plus.org": true, "viber.com": true, "forums.mydigitallife.net": true,
		"nsaneforums.com": true, "investsocial.com": true, "elevenlabs.io": true,
		"sora.com": true, "whoer.net": true, "c3pool.com": true, "devexpress.com": true,
		"meta.ai": true, "portal.lisk.com": true, "superbridge.app": true,
		"app.milkyway.zone": true, "app.nept.finance": true, "quasar.fi": true,
		"levana.finance": true, "aistudio.google.com": true, "ai.google.dev": true,
		"alkalimakersuite-pa.clients6.google.com": true, "horizen.io": true,
		"autogeriko.com": true, "w.wormhole.com": true, "curseforge.com": true,
		"weights.gg": true, "weights.com": true, "pump.fun": true, "indiakino.org": true,
		"tapecontent.net": true, "grok.com": true, "x.ai": true, "truthsocial.com": true,
		"spacex.com": true, "libgen.st": true, "libgen.rs": true, "books.ms": true,
		"myfonts.com": true, "terra.money": true, "dojo.trading": true, "astroport.fi": true,
		"assets.teinon.net": true, "pixwox.com": true, "pixnoy.com": true, "mantra.zone": true,
		"mynearwallet.com": true, "btdig.com": true, "readymag.com": true, "mintchain.io": true,
		"sndcdn.com": true, "arras.io": true, "nexusmods.com": true, "nexus-cdn.com": true,
		"faz.net": true, "spiegel.de": true, "free-barcode-generator.net": true,
		"xhcdn.com": true, "xvideos-cdn.com": true, "hoerzu.de": true, "dnb.com": true,
		"shop.gameloft.com": true, "creality.com": true, "imgxclub.com": true, "ti.com": true,
		"manus.im": true, "manuscdn.com": true, "notebooklm.google.com": true,
		"notebooklm.google": true, "bsky.app": true, "bigsv.ru": true, "musify.club": true,
		"supercell.com": true, "framer.com": true, "framerstatic.com": true,
		"framercanvas.com": true, "framerusercontent.com": true, "frmrspply.myshopify.com": true,
		"mangapicgallery.com": true, "bridgestone.com": true, "bridgestonemotorcycletires.com": true,
		"flourish.studio": true, "laratranslate.com": true, "miro.com": true, "kicker.de": true,
		"bundesliga.com": true, "nba.com": true, "d1vzi28wh99zvq.cloudfront.net": true,
		"tinkercad.com": true, "mantle.xyz": true, "mito.fi": true, "helixapp.com": true,
		"staking-explorer.com": true, "deepl.com": true, "highwebmedia.com": true,
		"autodesk.com": true, "milanote.com": true, "motor1.com": true, "newgrounds.com": true,
		"lookerstudio.google.com": true, "infineon.com": true, "linguee.de": true,
		"geonames.org": true, "runrepeat.com": true, "lichess.org": true, "00v.in": true,
		"mw5.community": true, "bourns.com": true, "te.com": true, "digikeyassets.com": true,
		"microsemi.com": true, "latticesemi.com": true, "imdb.com": true, "hatchcanvas.com": true,
		"mo.co": true, "pximg.net": true, "githubcopilot.com": true, "godaddy.com": true,
		"tablbrowser.com": true, "kemono.cr": true, "ariscommunity.com": true,
		"studiostaticassetsprod.azureedge.net": true, "pandasecurity.com": true,
		"bang-olufsen.com": true, "coomer.st": true, "gadgetversus.com": true, "itch.io": true,
		"unsplash.com": true, "montpellier.fr": true, "xerox.com": true, "periodika.lv": true,
		"digar.ee": true, "telegram.org": true, "telegram.me": true, "t.me": true,
		"telesco.pe": true, "ibb.co": true, "simgbb.com": true, "metalarea.org": true,
		"soquest.xyz": true, "oraidex.io": true, "mega.nz": true, "mega.co.nz": true,
		"mega.io": true, "spriters-resource.com": true, "parisaeroport.fr": true,
		"tuta.io": true, "tuta.com": true, "mf.life": true, "archdaily.com": true,
		"fracturae.com": true, "upload.ee": true, "umputun.com": true, "convertio.co": true,
		"mtgtop8.com": true, "fastpic.org": true, "metapix.net": true, "roland.com": true,
		"musicstore.com": true, "losslessclub.com": true, "mtgpics.com": true, "tesall.club": true,
		"kernel.org": true, "runwayml.com": true, "cellmapper.net": true, "club-nikon.ru": true,
		"images.musicstore.de": true, "merkl.xyz": true, "steinberg.net": true, "allcinema.net": true,
		"d-addicts.com": true, "puzzlegarage.com": true, "mui.com": true, "nordkeyboards.com": true,
		"hp.com": true, "askastrologer.org": true, "kinokong.li": true, "furtails.pw": true,
		"sograph.xyz": true, "walletlink.org": true, "sportsnet.ca": true, "tsn.ca": true,
		"720pier.ru": true, "espn.com": true, "cosmos-apis.com": true, "skip.money": true,
		"rudub.pics": true, "le-production.tv": true, "kmplayer.com": true, "onfinality.io": true,
		"rollapp.network": true, "toramp.com": true, "hdrezka-home.tv": true, "propio-ls.com": true,
		"elgato.com": true, "walletconnect.org": true, "binance.com": true, "binance.click": true,
		"nodies.app": true, "nodereal.io": true, "yshyqxx.com": true, "envatousercontent.com": true,
		"ngrok.com": true, "moonbeam.network": true, "kopi.money": true, "anidub.shop": true,
		"stories-cdn.fun": true, "chainik.io": true, "roblox.com": true, "rbxcdn.com": true,
		"hitmotop.com": true, "snapchat.com": true, "beatstars.com": true, "soundclick.com": true,
		"morkie.xyz": true, "bronbro.io": true, "download.revouninstaller.com": true,
		"kinosimka1.world": true, "yubsoft.com": true, "hhdsoftware.com": true, "crystalidea.com": true,
		"the-cinema.icu": true, "callofduty.com": true, "callofdutymobile.com": true,
		"codashop.com": true, "codainfra.com": true, "steamgifts.com": true, "steamstatic.com": true,
		"mobile.de": true, "classistatic.de": true, "flixster.com": true, "rottentomatoes.com": true,
		"hareruyamtg.com": true, "outfoxstories.com": true, "metabrainz.org": true, "fandango.com": true,
		"b-cdn.net": true, "adobedtm.com": true, "lezhin.com": true, "bomtoon.com": true,
		"balcony.studio": true, "mcomics.co.kr": true, "iamport.kr": true, "kakaocdn.net": true,
		"sniffmouse.com": true, "thenounproject.com": true, "gymnastics.sport": true, "gazzetta.it": true,
		"gazzettaobjects.it": true, "cloudconvert.com": true, "mtggoldfish.com": true,
		"forum-cdn.infinityfree.net": true, "trimble.com": true, "sketchup.com": true,
		"active.ridibooks.com": true, "milfnut.com": true, "onebookpublishing.org": true,
		"e-id.cards": true, "1001tracklists.com": true, "teamhd.org": true, "dl.bandicam.com": true,
		"rackcdn.com": true, "mirillis.com": true, "srji.org": true, "daumcdn.net": true,
		"forklog.com": true, "anwap-films.com": true, "userbenchmark.com": true, "shikimori.one": true,
		"acomics.ru": true, "dyinglightgame.com": true, "levelinfinite.com": true, "techland.gg": true,
		"foeru.innogamescdn.com": true, "community.pcgamingwiki.com": true, "digitaloceanspaces.com": true,
		"capcut.com": true, "the-cinema.city": true, "foreca.ru": true, "patreonusercontent.com": true,
		"mmcdn.com": true, "filevideo1.com": true, "987cdn.com": true, "trafficdeposit.com": true,
		"girlswithmuscle.com": true, "sb-cd.com": true, "forum.ru-board.com": true, "polymarket.com": true,
		"locoloader.com": true, "honor.com": true, "shure.com": true, "support.biamp.com": true,
		"crestron.com": true, "rustor.org": true, "ghostery.com": true, "ghostery.net": true,
		"last.fm": true, "ws.audioscrobbler.com": true, "lizardsystems.com": true, "threads.com": true,
		"images-assets.nasa.gov": true, "dreamerscast.com": true, "extensions.joomla.org": true,
		"porn-xp.com": true, "userstyles.world": true, "deviantart.net": true, "deviantart.com": true,
		"wordfence.com": true, "hashhedge.com": true, "doppiocdn.media": true, "vscdns.com": true,
		"figma.com": true, "seblod.com": true, "hikashop.com": true, "yootheme.com": true,
		"bigcommerce.com": true, "focusritegroup.com": true, "download.comodo.com": true,
		"anwap-films.site": true, "cdn.membrana.video": true, "nepchan.org": true,
		"family-guy.mult-fan.tv": true, "south-park.mult-fan.tv": true, "club-romance.ru": true,
		"lordserial.my": true, "kinogo.jp": true, "kinogo.webcam": true, "explore.org": true,
		"hochu.tv": true, "icy-veins.com": true, "steamdb.info": true, "bnbfree.in": true,
		"nnmstatic.win": true, "torrnado.space": true, "xn--80aizddian.xn--p1ai": true,
		"emcd.io": true, "languagelearning.site": true, "lostfilm.top": true, "insearch.site": true,
		"dessi.co": true,
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
				hostname, err := idnaAnalyse.ToASCII(hostname)
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
	response = nil
	fmt.Println("Parsed csv.")
	runtime.GC()

	// Дополнительный источник хостов (reestr_hostname_resolvable_ip4.txt)
	response = getOrDie("https://raw.githubusercontent.com/bol-van/rulist/refs/heads/main/reestr_hostname_resolvable_ip4.txt")
	fmt.Println("Downloaded additional hostname list.")

	scanner = bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
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

			hostname, err = idnaAnalyse.ToASCII(hostname)
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
			keys[i] = []int{addrToInt([]byte(mask.IP)), addrToInt([]byte(mask.Mask))}
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
	fmt.Fprintln(builder, "// "+newUpdateMessage)
	fmt.Println("Rendering template...")
	err = tmpl.ExecuteTemplate(builder, "template.js", struct{ INPUTS string }{INPUTS: string(marshalled)})
	if err != nil {
		panic(err)
	}
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
		Content: builder.String(),
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
	builder = nil
	body = nil
	readme = nil
	runtime.GC()

	doOrDie := func(method, url string, payload []byte) *http.Response {
		fmt.Println(method+"ing to", url)
		req, err := http.NewRequest(method, url, bytes.NewReader(payload))
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
			fmt.Println("Negative status code: " + strconv.Itoa(response.StatusCode) + ". For url: " + url)
			panic(method + " failed.")
		}
		fmt.Println(method + "ed.")
		return response
	}

	response = doOrDie("POST", REPO_URL+"/git/trees", marshalled)
	text, err = ioutil.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	response.Body.Close()
	tree := &struct {
		Sha string
	}{}
	json.Unmarshal(text, tree)
	marshalled = nil
	response = nil
	runtime.GC()

	commit := &GhCommit{
		Message: newUpdateMessage,
		Tree:    tree.Sha,
	}
	marshalled, err = json.Marshal(commit)
	if err != nil {
		panic(err)
	}
	response = doOrDie("POST", REPO_URL+"/git/commits", marshalled)
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
	response = doOrDie("PATCH", REPO_URL+"/git/refs/heads/master", marshalled)
	response.Body.Close()
	fmt.Println("Done.")
}
