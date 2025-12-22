package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"encoding/json"
	"text/template"

	"bytes"
	"encoding/binary"
	"net"

	"golang.org/x/net/idna"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

//var ifForced = flag.Bool("force", false, "If to ignore checking of an updated dump.csv available")
var ifForced = flag.Bool("force", true, "If to ignore checking of an updated dump.csv available")

type blockProvider struct {
	urls   []string
	rssUrl string
}

var blockProviders = []blockProvider{
	//blockProvider{
	//	urls: []string{
	//		"https://raw.githubusercontent.com/zapret-info/z-i/master/dump.csv",
	//	},
	//	rssUrl: "https://github.com/zapret-info/z-i/commits/master.atom",
	//},	
	blockProvider{
		urls: []string{
			"https://svn.code.sf.net/p/zapret-info/code/dump.csv",
		},
		rssUrl: "https://sourceforge.net/p/zapret-info/code/feed",
	},
	//blockProvider {
	//	urls: []string{
	//		"https://app.assembla.com/spaces/z-i/git/source/master/dump.csv?_format=raw",
	//	},
	//	rssUrl: "https://app.assembla.com/spaces/z-i/stream.rss",
	//},
}

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
		lastUpdateMessage = (*commits)[0].Commit.Message
	}
	var newUpdateMessage string

	updatedRegexp := regexp.MustCompile(`Updated: \d\d\d\d-\d\d-\d\d \d\d:\d\d:\d\d [+-]0000`)

	var bestProvider *blockProvider = nil
	for _, provider := range blockProviders {
		response, err := get(provider.rssUrl)
		if err != nil {
			fmt.Println("Skipping provider because of:", err)
			continue
		}
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			match := updatedRegexp.FindString(scanner.Text())
			if match != "" {
				if lastUpdateMessage < match {
					newUpdateMessage = match
					bestProvider = &provider
					break
				}
			}
		}
		if err := scanner.Err(); err != nil {
			panic(err)
		}
		response.Body.Close()
		if bestProvider != nil {
			break
		}
	}
	if bestProvider == nil {
		fmt.Println("No newer dump.csv published yet!")
		os.Exit(0)
	}
	urls := bestProvider.urls
	fmt.Println("Best provider urls are:", urls)

	// Ingored hostnames

	response = getOrDie("https://bitbucket.org/ValdikSS/antizapret/raw/master/ignorehosts.txt")
	fmt.Println("Downloaded ingoredhosts.")

	ignoredHostnames := map[string]bool{
  		"ynet.co.il": true,
		"www.ynet.co.il": true,
		"rutrk.org": true,
		"pornhub.com": true,
		"www.pornhub.com": true,
		"rt.pornhub.com": true,
		"ru.pornhub.com": true,
		"ytimg.com": true,
		"i.ytimg.com": true,
		"tiktok.com": true,
		"ggpht.com": true,
		"akamaihd.net": true,
		"youtube.com": true,
		"www.youtube.com": true,
		"googlevideo.com": true,
		".googlevideo.com": true,
		"prostovpn.org": true,
		"antizapret.prostovpn.org": true,
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		ignoredHostnames[scanner.Text()] = true
	}
	response.Body.Close()
	fmt.Println("Parsed ingoredhosts.txt.")

	// Not found hostnames

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

	// ТСПУ (TSPU), list of shaped hostnames
	
	//response = getOrDie("https://registry.censortracker.org/registry-api/domains/?countryCode=ru")
	//response = getOrDie("https://registry.censortracker.org/api/v3/dpi/")
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

	reader := csv.NewReader(transform.NewReader(csvIn, charmap.Windows1251.NewDecoder()))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1 // Don't check number of fields.
	idna := idna.New()
	customHostnames := map[string]bool{
		// TSPU-extra
		"ua": true, // Whole *.ua.
		// Extremism:
		"pravdabeslana.ru": true,
		// WordPress:
		"putinism.wordpress.com": true,
		"6090m01.wordpress.com":  true,
		// Custom hosts
		"archive.org": true,
		"bitcoin.org": true,
		// LinkedIn
		"licdn.com":    true,
		"linkedin.com": true,
		// Based on users complaints:
		"koshara.net":     true,
		"koshara.co":      true,
		"new-team.org":    true,
		"fast-torrent.ru": true,
		"pornreactor.cc":  true,
		"vn.reactor.cc":   true,
		"vatnik.reactor.cc":   true,
		"nnm-club.name":   true,
		"rutor.info":      true,
		"free-rutor.org":  true,
		"doramacine.in":  true,
		// Rutracker complaints:
		"static.t-ru.org": true,
		

		"nnm-club.ws":    true,
		"lostfilm.tv":    true,
		"e-hentai.org":   true,
		"deviantart.net": true, 	// https://groups.google.com/forum/#!topic/anticensority/uXFsOS1lQ2
		"kaztorka.org":   true, 	// https://groups.google.com/forum/#!msg/anticensority/vweNToREQ1o/3EbhCDjfAgAJ
		"familysearch.org": true,
		"fastproxy.online": true,
		"dlinkddns.com": true, 		// 451 - blocked in RU
		"padlet.com": true, 		// ТСПУ
		"ionos.com": true, 		// ТСПУ
		"alphacoders.com": true,	// 451 - blocked in RU
		"claude.ai": true,		// 451 - blocked in RU
		"reka.ai": true,		// 451 - blocked in RU
		"timehd.club": true, 		// ТСПУ
		"rebrand.ly": true, 		// ТСПУ
		"skycdp.com": true,		// 403 - blocked in RU
		"wildstat.com": true,		// blocked in RU
		"tria.ge": true, 		// blocked in RU
		"rulate.ru": true,		// content blocked in RU
		"fanficus.com": true, 		// ТСПУ
		"fanficus-server-mirror-879c30cd977f.herokuapp.com": true,		// domain checks GEO for site fanficus.com
		"apollo.farm": true,		// blocked in RU
		"station.money": true,		// blocked in RU
		"skip.build": true,		// content blocked in RU
		"iar.com": true,		// content blocked in RU
		"st.com": true,			// content blocked in RU
		"howlongtobeat.com": true,	// blocked in RU
		"lingq.com": true, 		// ТСПУ
		"thealphacentauri.net": true, 	// ТСПУ
		"notion.so": true,		// content blocked in RU
		"clickup.com": true,		// content blocked in RU
		"recraft.ai": true,		// content blocked in RU
		"kinoplay2.site": true,		// ТСПУ
		"gethomebank.org": true,	// blocked in RU
		"themoviedb.org": true,		// blocked in RU
		"optimism.io": true,		// blocked in RU
		"documentingreality.com": true,	// blocked in RU
		"coda.io": true,		// blocked in RU
		"puffer.fi": true,		// blocked in RU
		"kamino.finance": true,		// content blocked in RU
		"debridge.foundation": true,	// blocked in RU
		"zora.co": true,		// blocked in RU
		"govgen.io": true,		// blocked in RU
		"saga.xyz": true,		// blocked in RU
		"grassfoundation.io": true,	// content blocked in RU
		"getgrass.io": true,		// check ip for grassfoundation.io
		//"forum.ru-board.com": true,	// blocked in RU
		"orbit-games.com": true,	// blocked in RU
		"hetzner.com": true,		// ТСПУ
		"audiobookbay.lu": true,	// ТСПУ
		"humblebundle.com": true,	// blocked in RU
		"capacitorjs.com": true,	// blocked in RU
		"blur.io": true,		// blocked in RU
		"notepad-plus-plus.org": true,	// blocked in RU
		"viber.com": true,		// ТСПУ
		"forums.mydigitallife.net": true,	// ТСПУ
		"nsaneforums.com": true,	// ТСПУ
		"investsocial.com": true,	// ТСПУ
		"elevenlabs.io": true,		// blocked in RU
		"sora.com": true,		// blocked in RU
		"whoer.net": true,		// blocked in RU
		"c3pool.com": true,		// ТСПУ
		"devexpress.com": true,		// blocked in RU
		"meta.ai": true,		// blocked in RU
		"portal.lisk.com": true,	// blocked in RU
		"superbridge.app": true,	// blocked in RU
		"app.milkyway.zone": true,	// blocked in RU
		"app.nept.finance": true,	// blocked in RU
		"quasar.fi": true,		// blocked in RU
		"levana.finance": true,		// blocked in RU
		"aistudio.google.com": true,	// blocked in RU
		"ai.google.dev": true,		// blocked in RU
		//"alkalimakersuite-pa.clients6.google.com": true,		// domain checks GEO for site aistudio.google.com
		//"waa-pa.clients6.google.com": true,		// domain checks GEO for site aistudio.google.com
		"horizen.io": true,		// blocked in RU
		"autogeriko.com": true,		// blocked in RU
		"w.wormhole.com": true,		// blocked in RU
		"curseforge.com": true,		// 403 - blocked in RU
		"weights.gg": true,		// 403 - blocked in RU
		"weights.com": true,		// 403 - blocked in RU
		"pump.fun": true,		// 403 - blocked in RU
		"indiakino.org": true,		// ТСПУ
		"tapecontent.net": true,	// 403 - blocked in RU
		"grok.com": true,		// blocked in RU
		"x.ai": true,			// blocked in RU
		"truthsocial.com": true,	// 403 - blocked in RU
		"spacex.com": true,		// content blocked in RU
		"libgen.st": true,		// host in Ukraine
		"libgen.rs": true,		// host in Ukraine
		"books.ms": true,		// host in Ukraine
		"myfonts.com": true,		// 403 - blocked in RU
		"terra.money": true,		// blocked in RU
		"dojo.trading": true,		// blocked in RU
		"astroport.fi": true,		// blocked in RU
		"assets.teinon.net": true,	// blocked in RU
		"pixwox.com": true,		// blocked in RU
		"pixnoy.com": true,		// blocked in RU
		"mantra.zone": true,		// blocked in RU
		"mynearwallet.com": true,	// blocked in RU
		"btdig.com": true,		// blocked in RU
		"readymag.com": true,		// blocked in RU
		"mintchain.io": true,		// blocked in RU
		"sndcdn.com": true,		// ТСПУ
		"arras.io": true,		// ТСПУ
		"nexusmods.com": true,		// ТСПУ
		"nexus-cdn.com": true,		// ТСПУ
		"faz.net": true,		// ТСПУ
		"spiegel.de": true,		// ТСПУ
		"free-barcode-generator.net": true,	// blocked in RU
		"xhcdn.com": true,		// ТСПУ
		"xvideos-cdn.com": true,	// ТСПУ
		"hoerzu.de": true,		// ТСПУ
		"dnb.com": true,		// blocked in RU
		"shop.gameloft.com": true,	// blocked in RU
		"creality.com": true,		// ТСПУ
		"imgxclub.com": true,		// ТСПУ
		"ti.com": true,			// ТСПУ
		"manus.im": true,		// blocked in RU
		"manuscdn.com": true,		// Amazon
		"notebooklm.google.com": true,	// blocked in RU
		"notebooklm.google": true,	// blocked in RU
		"bsky.app": true,		// ТСПУ
		"bigsv.ru": true,		// ТСПУ - css/js for seasonvar.ru
		"musify.club": true,		// ТСПУ
		"supercell.com": true,		// blocked in RU
		"framer.com": true,		// blocked in RU
		"framerstatic.com": true,		// Amazon
		"framercanvas.com": true,		// Amazon
		"framerusercontent.com": true,		// Amazon
		"frmrspply.myshopify.com": true,	//Cloudflare
		"mangapicgallery.com": true,	// ТСПУ
		"bridgestone.com": true,	// blocked in RU
		"bridgestonemotorcycletires.com": true,	// blocked in RU
		"flourish.studio": true,	// blocked in RU
		"laratranslate.com": true,	// ТСПУ
		"miro.com": true,		// ТСПУ
		"kicker.de": true,		// blocked in RU
		"bundesliga.com": true,		// ТСПУ
		"nba.com": true,		// blocked in RU
		"d1vzi28wh99zvq.cloudfront.net": true,		// images for drivethrurpg.com
		"tinkercad.com": true,		// ТСПУ
		"mantle.xyz": true,		// ТСПУ
		"mito.fi": true,		// ТСПУ
		"helixapp.com": true,		// ТСПУ
		"staking-explorer.com": true,	// ТСПУ
		"deepl.com": true,		// blocked in RU
		"highwebmedia.com": true,	// wss for chaturbate.com
		"autodesk.com": true,		// blocked in RU
		"milanote.com": true,		// ТСПУ
		"motor1.com": true,		// blocked in RU
		"newgrounds.com": true,		// ТСПУ
		"lookerstudio.google.com": true,	// blocked in RU
		"infineon.com": true,		// blocked in RU
		"linguee.de": true,		// blocked in RU
		"geonames.org": true,		// ТСПУ
		"runrepeat.com": true,		// ТСПУ
		"lichess.org": true,		// ТСПУ
		"00v.in": true,			// ТСПУ
		"mw5.community": true,		// blocked in RU
		"bourns.com": true,		// blocked in RU
		"te.com": true,			// blocked in RU
		"digikeyassets.com": true,	// ТСПУ
		"microsemi.com": true,		// blocked in RU
		"latticesemi.com": true,	// blocked in RU
		"imdb.com": true,		// ТСПУ
		"hatchcanvas.com": true,	// blocked in RU
		"mo.co": true,			// blocked in RU
		"pximg.net": true,		// ТСПУ
		"githubcopilot.com": true,	// blocked in RU
		"godaddy.com": true,		// ТСПУ
		"tablbrowser.com": true,	// blocked in RU
		"kemono.cr": true,	// blocked in RU
		"ariscommunity.com": true,	// blocked downloads in RU
		"studiostaticassetsprod.azureedge.net": true,		// ТСПУ - static content for copilot.microsoft.com
		"pandasecurity.com": true,	// blocked in RU
		"bang-olufsen.com": true,	// blocked in RU
		"coomer.st": true,	// blocked in RU
		"gadgetversus.com": true,	// blocked in RU
		"itch.io": true,		// ТСПУ
		"unsplash.com": true,		// ТСПУ
		"montpellier.fr": true,	// blocked in RU
		"xerox.com": true,	// blocked in RU
		"periodika.lv": true,	// blocked in RU
		"digar.ee": true,	// blocked in RU
		"telegram.org": true,		// ТСПУ
		"ibb.co": true,		// ТСПУ
		"simgbb.com": true,		// ТСПУ
		"metalarea.org": true,		// ТСПУ
		"soquest.xyz": true,		// ТСПУ
		"oraidex.io": true,		// ТСПУ
		"mega.nz": true,		// ТСПУ
		"mega.co.nz": true,		// ТСПУ
		"mega.io": true,		// ТСПУ
		"spriters-resource.com": true,		// ТСПУ
		"parisaeroport.fr": true,	// blocked in RU
		"tuta.io": true,		// ТСПУ
		"tuta.com": true,		// ТСПУ
		"mf.life": true,		// ТСПУ
		"archdaily.com": true,		// ТСПУ
		"fracturae.com": true,		// ТСПУ
		"upload.ee": true,		// ТСПУ
		"umputun.com": true,		// ТСПУ
		"convertio.co": true,		// ТСПУ
		"mtgtop8.com": true,		// ТСПУ
		"fastpic.org": true,		// ТСПУ
		"metapix.net": true,		// ТСПУ
		"roland.com": true,		// ТСПУ
		"musicstore.com": true,		// ТСПУ
		"losslessclub.com": true,		// ТСПУ
		"mtgpics.com": true,		// ТСПУ
		"tesall.club": true,		// Hetzner
		"kernel.org": true,		// ТСПУ
		"runwayml.com": true,		// Amazon
		"cellmapper.net": true,		// OVH
		"club-nikon.ru": true,		// Hetzner
		"images.musicstore.de": true,		// Akamai
		"merkl.xyz": true,		// ТСПУ
		"steinberg.net": true,		// Amazon
		"allcinema.net": true,		// Contabo
		"d-addicts.com": true,		// OVH
		"puzzlegarage.com": true,		// Hetzner
		"mui.com": true,		// Amazon
		"nordkeyboards.com": true,		// Cloudflare
		"hp.com": true,		// Amazon
		"askastrologer.org": true,		// Hetzner
		"kinokong.li": true,		// РКН
		"furtails.pw": true,		// DigitalOcean
		"sograph.xyz": true,		// Amazon
		"walletlink.org": true,		// Cloudflare
		"sportsnet.ca": true,		// Akamai
		"tsn.ca": true,		// Akamai
		"720pier.ru": true,		// Crea Nova
		"espn.com": true,		// Amazon
		"cosmos-apis.com": true,		// Cloudflare
		"skip.money": true,		// Amazon
		"rudub.pics": true,		// ТСПУ
		"le-production.tv": true,		// ТСПУ
		"kmplayer.com": true,		// Google Cloud
		"onfinality.io": true,		// Amazon
		"rollapp.network": true,		// Google Cloud
		"toramp.com": true,		// Akamai
		//"web.whatsapp.com": true,		// ТСПУ
		"hdrezka-home.tv": true,		// ТСПУ
		"propio-ls.com": true,		// Amazon
		"elgato.com": true,		// Akamai
		"walletconnect.org": true,		// Cloudflare
		"binance.com": true,		// Amazon
		"binance.click": true,		// ТСПУ
		"nodies.app": true,		// Cloudflare
		"nodereal.io": true,		// Amazon
		"yshyqxx.com": true,		// ТСПУ
		"envatousercontent.com": true,		// Amazon
		"ngrok.com": true,		// Amazon
		"moonbeam.network": true,		// Amazon
		"kopi.money": true,		// Hetzner
		"anidub.shop": true,		// Scalaxy
		"stories-cdn.fun": true,		// Hetzner
		"chainik.io": true,		// Hetzner
		"roblox.com": true,		// Roblox
		"rbxcdn.com": true,		// Amazon
		"hitmotop.com": true,		// ТСПУ
		"snapchat.com": true,		// ТСПУ
		"beatstars.com": true,		// Amazon
		"soundclick.com": true,		// Amazon
		"morkie.xyz": true,		// Amazon
		"bronbro.io": true,		// Hetzner
		"download.revouninstaller.com": true,		// Akamai
		"kinosimka1.world": true,		// EuroHoster
		"yubsoft.com": true,		// Vultr
		"hhdsoftware.com": true,		// Hetzner
		"crystalidea.com": true,		// Akamai
		"the-cinema.icu": true,		// Cogent
		"callofduty.com": true,		// Amazon
		"callofdutymobile.com": true,		// Amazon
		"codashop.com": true,		// Amazon
		"codainfra.com": true,		// Amazon
		"steamgifts.com": true,		// Amazon
		"steamstatic.com": true,		// Amazon
		"mobile.de": true,		// Amazon
		"classistatic.de": true,		// Amazon
		"flixster.com": true,		// Amazon
		"rottentomatoes.com": true,		// Amazon
		"hareruyamtg.com": true,		// Amazon
		"outfoxstories.com": true,		// DigitalOcean
		"metabrainz.org": true,		// Hetzner
		"fandango.com": true,		// Akamai
		"b-cdn.net": true,		// ТСПУ
		"adobedtm.com": true,		// Akamai
		"lezhin.com": true,		// Amazon
		"bomtoon.com": true,		// Amazon
		"balcony.studio": true,		// Amazon
		"mcomics.co.kr": true,		// Amazon
		"iamport.kr": true,		// Amazon
		"kakaocdn.net": true,		// Amazon
		"sniffmouse.com": true,		// DigitalOcean
		"thenounproject.com": true,		// Amazon
		"gymnastics.sport": true,		// Amazon
		"gazzetta.it": true,		// Amazon
		"gazzettaobjects.it": true,		// Amazon
		"cloudconvert.com": true,		// Amazon
		"mtggoldfish.com": true,		// Amazon
		"forum-cdn.infinityfree.net": true,		// Amazon
		"trimble.com": true,		// Amazon
		"sketchup.com": true,		// Amazon
		"active.ridibooks.com": true,		// Amazon
		"milfnut.com": true,		// ТСПУ
		"onebookpublishing.org": true,		// ТСПУ
		
		// ECH (CloudFlare) ТСПУ
		"icy-veins.com": true,	
		"steamdb.info": true,
		"bnbfree.in": true,
		//"surasoft.ru": true,
		"nnmstatic.win": true,
		"torrnado.space": true,
		"xn--80aizddian.xn--p1ai": true,
		"emcd.io": true,
		"languagelearning.site": true,
		//"joyreactor.cc": true,
		"lostfilm.top": true,
		"insearch.site": true,
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
				hostname, err := idna.ToASCII(hostname)
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

	// Converts IP mask to 16 bit unsigned integer.
	addrToInt := func(in []byte) int {

		//var i uint16
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
	//ipv6Map := getOptimizedMap(ipv6)
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
	//out, in := io.Pipe()
	//defer in.Close()
	//defer out.Close()

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
			fmt.Println(response.Body)
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
