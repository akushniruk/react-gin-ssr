package handlers

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/foolin/goview/supports/ginview"
	"github.com/gin-gonic/gin"
	"github.com/openware/kaigara/pkg/vault"
	"github.com/openware/sonic"

	"github.com/akushniruk/react-gin-ssr/render"
	"github.com/akushniruk/react-gin-ssr/postcode"
)

var config *Config
var engine *render.Engine

type Config struct {
	polyfillLocation string
	scriptLocation   string
	templateLocation string
	staticDir        string
	port             string
}

func init() {
	c := new(Config)

	pwd, _ := os.Getwd()
	c.polyfillLocation = pwd + "/react-src/react-build/duktape-polyfill.js"
	c.scriptLocation = pwd + "/react-src/react-build/static/js/server.js"
	c.templateLocation = pwd + "/react-src/react-build/index.html"
	c.staticDir = pwd + "/react-src/react-build"
	c.port = "8080"
	config = c

	engine = render.NewEngine(c.polyfillLocation, c.scriptLocation, c.templateLocation)
}

// Version variable stores Application Version from main package
var (
	Version      string
	DeploymentID string
	memoryCache  = cache{
		Data:  make(map[string]map[string]interface{}),
		Mutex: sync.RWMutex{},
	}
)

// Initialize scope which goroutine will fetch every 30 seconds
const scope = "public"

// Setup set up routes to render view HTML
func Setup(app *sonic.Runtime) {

	router := app.Srv
	// Set up view engine
	router.HTMLRender = ginview.Default()
	Version = app.Version
	vaultConfig := app.Conf.Vault
	DeploymentID = app.Conf.DeploymentID

	log.Println("DeploymentID in config:", app.Conf.DeploymentID)

	// Serve static files
	fs := http.FileServer(http.Dir(config.staticDir))

	http.Handle("/static/js/", fs)
	http.Handle("/static/css/", fs)
	http.Handle("/images/", fs)

	http.HandleFunc("/postcode/", postcode.HandlePostcodeQuery)
	http.HandleFunc("/", handleDynamicRoute)

	SetPageRoutes(router)

	vaultMiddleware := VaultConfigMiddleware(&vaultConfig)

	vaultAPI := router.Group("/api/v2/admin")
	vaultAPI.Use(vaultMiddleware)
	vaultAPI.GET("/secrets", GetSecrets)

	vaultAPI.PUT(":component/secret", SetSecret)

	vaultPublicAPI := router.Group("/api/v2/public")
	vaultPublicAPI.Use(vaultMiddleware)

	vaultPublicAPI.GET("/config", GetPublicConfigs)

	// Initialize Vault Service
	vaultService := vault.NewService(vaultConfig.Addr, vaultConfig.Token, "global", DeploymentID)

	// Define all public env on first system start
	WriteCache(vaultService, scope, true)
	go StartConfigCaching(vaultService, scope)
}

// StartConfigCaching will fetch latest data from vault every 30 seconds
func StartConfigCaching(vaultService *vault.Service, scope string) {
	for {
		<-time.After(30 * time.Second)

		memoryCache.Mutex.Lock()
		WriteCache(vaultService, scope, false)
		memoryCache.Mutex.Unlock()
	}
}

// index render with master layer
func index(ctx *gin.Context) {
	cssFiles, err := FilesPaths("/public/assets/*.css")
	if err != nil {
		log.Println("filePaths:", "Can't take list of paths for css files: "+err.Error())
	}

	jsFiles, err := FilesPaths("/public/assets/*.js")
	if err != nil {
		log.Println("filePaths", "Can't take list of paths for js files in public folder: "+err.Error())
	}

	ctx.HTML(http.StatusOK, "index", gin.H{
		"title":    "Index title!",
		"cssFiles": cssFiles,
		"jsFiles":  jsFiles,
		"rootID":   "root",
		"add": func(a int, b int) int {
			return a + b
		},
	})
}

// render only file, must full name with extension
func emptyPage(ctx *gin.Context) {
	ctx.HTML(http.StatusOK, "page.html", gin.H{"title": "Page file title!!"})
}

// Return application version
func version(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{"Version": Version})
}

func FilesPaths(pattern string) ([]string, error) {
	var matches []string

	fullPath, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	matches, err = filepath.Glob(fullPath + pattern)
	if err != nil {
		return nil, err
	}

	for i, _ := range matches {
		matches[i] = strings.Replace(matches[i], fullPath, "", -1)
	}

	return matches, nil
}


func handleDynamicRoute(w http.ResponseWriter, r *http.Request) {
	renderedTemplate := engine.Render(r.URL.Path, resolveServerSideState())
	w.Write([]byte(renderedTemplate))
}

type serverSideState struct {
	PostcodeQuery string              `json:"postcodeQuery"`
	Postcodes     []postcode.Postcode `json:"postcodes"`
}

func resolveServerSideState() string {

	initialPostcode := "ST3"

	serverSideState := serverSideState{}
	serverSideState.PostcodeQuery = initialPostcode
	serverSideState.Postcodes = postcode.FetchPostcodes(initialPostcode)

	serverSideStateJSON, err := json.Marshal(serverSideState)
	if err != nil {
		// TODO Handle this properly
		fmt.Println(err)
	}

	return string(serverSideStateJSON)
}
