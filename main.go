package main

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	CAPassword = "123456"
	Bind       = ":8080"
	AllowedIPs = []string{"127.0.0.1"}
)

var ReinitializeAuthKey = "0603762D-E368-4EC3-800B-5819A8BF3E0C"

func init() {
	KocoBind := os.Getenv("KOCO_BIND")
	if KocoBind != "" {
		Bind = KocoBind
	}

	KocoAllowedIp := os.Getenv("KOCO_ALLOWED_IP")
	if KocoAllowedIp != "" {
		AllowedIPs = strings.Split(KocoAllowedIp, ",")
	}

	OvpnCaPassword := os.Getenv("OVPN_CA_PASSWORD")
	if OvpnCaPassword != "" {
		CAPassword = OvpnCaPassword
	}
}

func IndexWithError(ctx *gin.Context, err error) {
	var errorSet []error
	if err != nil {
		errorSet = append(errorSet, err)
	}
	clients, err := ListClients()
	if err != nil {
		errorSet = append(errorSet, err)
	}
	ctx.HTML(http.StatusOK, "index.html", gin.H{
		"Errors":              errorSet,
		"Clients":             clients,
		"ReinitializeAuthKey": ReinitializeAuthKey,
	})
}

func ErrorPage(ctx *gin.Context, code int, err error) {
	ctx.HTML(code, "error.html", gin.H{
		"Errors": []error{err},
	})
}

type ClientForm struct {
	Name string `form:"name"`
	Pass string `form:"pass"`
}

func main() {
	router := gin.Default()

	router.Use(func(ctx *gin.Context) {
		if !slices.Contains[[]string](AllowedIPs, ctx.ClientIP()) {
			ErrorPage(ctx, http.StatusUnauthorized, errors.New("permission denied"))
			ctx.Abort()
		}
	})

	router.SetFuncMap(template.FuncMap{
		"urlescaper":  template.URLQueryEscaper,
		"htmlescaper": template.HTMLEscapeString,
	})
	router.LoadHTMLGlob("templates/*")

	router.GET("/", func(ctx *gin.Context) {
		IndexWithError(ctx, nil)
	})
	router.GET("/index", func(ctx *gin.Context) {
		IndexWithError(ctx, nil)
	})

	router.GET("/download", func(ctx *gin.Context) {
		name := strings.TrimSpace(ctx.Query("name"))
		if name == "" {
			ErrorPage(ctx, http.StatusBadRequest, errors.New("name is required for downloading .ovpn file"))
			return
		}
		ovpnContent, err := GetClient(name)
		if err != nil {
			ErrorPage(ctx, http.StatusInternalServerError, err)
			return
		}
		tmpFile, err := os.CreateTemp(os.TempDir(), "koco_*.ovpn")
		if err != nil {
			ErrorPage(ctx, http.StatusInternalServerError, err)
			return
		}
		_, err = tmpFile.Write([]byte(ovpnContent))
		if err != nil {
			ErrorPage(ctx, http.StatusInternalServerError, err)
			return
		}
		defer func() {
			_ = os.Remove(tmpFile.Name())
		}()
		ctx.FileAttachment(tmpFile.Name(), fmt.Sprintf("%s.ovpn", name))
	})

	router.GET("/add", func(ctx *gin.Context) {
		ctx.HTML(http.StatusOK, "add.html", gin.H{})
	})
	router.POST("/add.do", func(ctx *gin.Context) {
		clientForm := ClientForm{}
		err := ctx.Bind(&clientForm)
		clientForm.Name = strings.TrimSpace(clientForm.Name)
		if err != nil {
			ctx.HTML(http.StatusOK, "add.html", gin.H{
				"Errors":     []error{err},
				"ClientForm": clientForm,
			})
			return
		} else if clientForm.Name == "" {
			ctx.HTML(http.StatusOK, "add.html", gin.H{
				"Errors":     []error{errors.New("name must not be empty")},
				"ClientForm": clientForm,
			})
			return
		} else if ok, _ := regexp.MatchString("^\\w+$", clientForm.Name); !ok {
			ctx.HTML(http.StatusOK, "add.html", gin.H{
				"Errors":     []error{errors.New("name is not valid")},
				"ClientForm": clientForm,
			})
			return
		}
		err = BuildClientFull(CAPassword, clientForm.Name, clientForm.Pass)
		if err != nil {
			ctx.HTML(http.StatusOK, "add.html", gin.H{
				"Errors":     []error{err},
				"ClientForm": clientForm,
			})
			return
		}
		ctx.Redirect(http.StatusSeeOther, "/")
	})

	router.GET("/delete", func(ctx *gin.Context) {
		name := strings.TrimSpace(ctx.Query("name"))
		if name == "" {
			ErrorPage(ctx, http.StatusBadRequest, errors.New("client name is required for revoking"))
			return
		}
		err := RevokeClient(CAPassword, name)
		if err != nil {
			ErrorPage(ctx, http.StatusInternalServerError, err)
			return
		} else {
			ctx.Redirect(http.StatusSeeOther, "/")
		}
	})

	router.GET("/reinitialize", func(ctx *gin.Context) {
		key := strings.TrimSpace(ctx.Query("key"))
		if key != ReinitializeAuthKey {
			ErrorPage(ctx, http.StatusBadRequest, errors.New("key is not valid"))
			return
		}
		err := Initialize(CAPassword)
		if err != nil {
			ErrorPage(ctx, http.StatusInternalServerError, err)
			return
		}
		ctx.Redirect(http.StatusSeeOther, "/")
	})

	_ = router.Run(Bind)
}