package main

import (
	"context"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"alexheld.io/cmd/site/internal/middleware"
	"alexheld.io/jsonfeed"
	"github.com/gorilla/feeds"
	_ "github.com/joho/godotenv/autoload"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	blackfriday "github.com/russross/blackfriday"
	"github.com/sebest/xff"
	"github.com/snabb/sitemap"
	"within.website/ln"
	"within.website/ln/ex"
	"within.website/ln/opname"

	"alexheld.io/cmd/site/internal/blog"
)

var port = os.Getenv("PORT")

func main() {
	if port == "" {
		port = "29384"
	}

	ctx := ln.WithF(opname.With(context.Background(), "main"), ln.F{
		"port":    port,
		"git_rev": gitRev,
	})

	//	_ = prometheus.Register(prommod.NewCollector("christine"))

	s, err := Build()
	if err != nil {
		fmt.Printf("Site= %+v\nErr=%+v\nPort=%d\n", s, err.Error(), port)
		ln.FatalErr(ctx, err, ln.Action("Build"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.within/health", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "OK", http.StatusOK)
	})
	mux.Handle("/", s)

	ln.Log(ctx, ln.Action("http_listening"))
	ln.FatalErr(ctx, http.ListenAndServe(":"+port, mux))
}

// Site is the parent object for https://alexheld.io's backend.
type Site struct {
	Posts       blog.Posts
	Talks       blog.Posts
	Gallery     blog.Posts
	Resume      template.HTML
	Series      []string
	SignalBoost []Person

	clacks   ClackSet
	patrons  []string
	rssFeed  *feeds.Feed
	jsonFeed *jsonfeed.Feed

	mux   *http.ServeMux
	xffmw *xff.XFF
}

var gitRev = os.Getenv("GIT_REV")

func envOr(key, or string) string {
	if result, ok := os.LookupEnv(key); ok {
		return result
	}

	return or
}

func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := opname.With(r.Context(), "site.ServeHTTP")
	ctx = ln.WithF(ctx, ln.F{
		"user_agent": r.Header.Get("User-Agent"),
	})
	r = r.WithContext(ctx)
	if gitRev != "" {
		w.Header().Add("X-Git-Rev", gitRev)
	}

	w.Header().Add("X-Hacker", "If you are reading this, check out /signalboost to find people for your team")

	s.clacks.Middleware(
		middleware.RequestID(
			s.xffmw.Handler(
				ex.HTTPLog(s.mux),
			),
		),
	).ServeHTTP(w, r)
}

var arbDate = time.Date(2020, time.May, 21, 0, 0, 0, 0, time.UTC)

// Build creates a new Site instance or fails.
func Build() (*Site, error) {

	_ = os.Setenv("PATREON_CLIENT_ID", "1234")
	_ = os.Setenv("PATREON_CLIENT_SECRET", "1234")
	_ = os.Setenv("PATREON_ACCESS_TOKEN", "1234")
	_ = os.Setenv("PATREON_REFRESH_TOKEN", "1234")
	pc, err := NewPatreonClient()
	if err != nil {
		return nil, err
	}

	pledges, err := GetPledges(pc)
	if err != nil {
		return nil, err
	}

	people, err := loadPeople("./signalboost.dhall")
	if err != nil {
		return nil, err
	}

	smi := sitemap.New()
	smi.Add(&sitemap.URL{
		Loc:        "https://alexheld.io/resume",
		LastMod:    &arbDate,
		ChangeFreq: sitemap.Monthly,
	})

	smi.Add(&sitemap.URL{
		Loc:        "https://alexheld.ioe/contact",
		LastMod:    &arbDate,
		ChangeFreq: sitemap.Monthly,
	})

	smi.Add(&sitemap.URL{
		Loc:        "https://alexheld.io/",
		LastMod:    &arbDate,
		ChangeFreq: sitemap.Monthly,
	})

	smi.Add(&sitemap.URL{
		Loc:        "https://alexheld.io/patrons",
		LastMod:    &arbDate,
		ChangeFreq: sitemap.Weekly,
	})

	smi.Add(&sitemap.URL{
		Loc:        "https://alexheld.io/blog",
		LastMod:    &arbDate,
		ChangeFreq: sitemap.Weekly,
	})

	xffmw, err := xff.Default()
	if err != nil {
		return nil, err
	}

	s := &Site{
		rssFeed: &feeds.Feed{
			Title:       "Alexander Held's Blog",
			Link:        &feeds.Link{Href: "https://alexheld.io/blog"},
			Description: "My blog posts and rants about various technology things.",
			Author:      &feeds.Author{Name: "Alexander Held", Email: "contact@alexheld.io"},
			Created:     bootTime,
			Copyright:   "This work is copyright Alexander Held. My viewpoints are my own and not the view of any employer past, current or future.",
		},
		jsonFeed: &jsonfeed.Feed{
			Version:     jsonfeed.CurrentVersion,
			Title:       "Alexander Held's Blog",
			HomePageURL: "https://alexheld.io",
			FeedURL:     "https://alexheld.io/blog.json",
			Description: "My blog posts and rants about various technology things.",
			UserComment: "This is a JSON feed of my blogposts. For more information read: https://jsonfeed.org/version/1",
			Icon:        icon,
			Favicon:     icon,
			Author: jsonfeed.Author{
				Name:   "Alexander Held",
				Avatar: icon,
			},
		},
		mux:   http.NewServeMux(),
		xffmw: xffmw,

		clacks:      ClackSet(strings.Split(envOr("CLACK_SET", "Ashlynn"), ",")),
		patrons:     pledges,
		SignalBoost: people,
	}

	posts, err := blog.LoadPosts("./blog/", "blog")
	if err != nil {
		return nil, err
	}
	s.Posts = posts
	s.Series = posts.Series()
	sort.Strings(s.Series)

	talks, err := blog.LoadPosts("./talks", "talks")
	if err != nil {
		return nil, err
	}
	s.Talks = talks

	gallery, err := blog.LoadPosts("./gallery", "gallery")
	if err != nil {
		return nil, err
	}
	s.Gallery = gallery

	var everything blog.Posts
	everything = append(everything, posts...)
	everything = append(everything, talks...)
	everything = append(everything, gallery...)

	sort.Sort(sort.Reverse(everything))

	resumeData, err := ioutil.ReadFile("./static/resume/resume.md")
	if err != nil {
		return nil, err
	}

	s.Resume = template.HTML(blackfriday.Run(resumeData))

	for _, item := range everything {
		s.rssFeed.Items = append(s.rssFeed.Items, &feeds.Item{
			Title:       item.Title,
			Link:        &feeds.Link{Href: "https://alexheld.io/" + item.Link},
			Description: item.Summary,
			Created:     item.Date,
			Content:     string(item.BodyHTML),
		})

		s.jsonFeed.Items = append(s.jsonFeed.Items, jsonfeed.Item{
			ID:            "https://alexheld.io/" + item.Link,
			URL:           "https://alexheld.io/" + item.Link,
			Title:         item.Title,
			DatePublished: item.Date,
			ContentHTML:   string(item.BodyHTML),
			Tags:          item.Tags,
		})

		smi.Add(&sitemap.URL{
			Loc:        "https://alexheld.io/" + item.Link,
			LastMod:    &item.Date,
			ChangeFreq: sitemap.Monthly,
		})
	}

	// Add HTTP routes here
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			s.renderTemplatePage("error.html", "can't find "+r.URL.Path).ServeHTTP(w, r)
			return
		}

		s.renderTemplatePage("index.html", nil).ServeHTTP(w, r)
	})
	s.mux.Handle("/metrics", promhttp.Handler())
	s.mux.Handle("/feeds", middleware.Metrics("feeds", s.renderTemplatePage("feeds.html", nil)))
	//s.mux.Handle("/patrons", middleware.Metrics("patrons", s.renderTemplatePage("patrons.html", s.patrons)))
	s.mux.Handle("/signalboost", middleware.Metrics("signalboost", s.renderTemplatePage("signalboost.html", s.SignalBoost)))
	s.mux.Handle("/resume", middleware.Metrics("resume", s.renderTemplatePage("resume.html", s.Resume)))
	s.mux.Handle("/blog", middleware.Metrics("blog", s.renderTemplatePage("blogindex.html", s.Posts)))
	s.mux.Handle("/talks", middleware.Metrics("talks", s.renderTemplatePage("talkindex.html", s.Talks)))
	s.mux.Handle("/gallery", middleware.Metrics("gallery", s.renderTemplatePage("galleryindex.html", s.Gallery)))
	s.mux.Handle("/contact", middleware.Metrics("contact", s.renderTemplatePage("contact.html", nil)))
	s.mux.Handle("/blog.rss", middleware.Metrics("blog.rss", http.HandlerFunc(s.createFeed)))
	s.mux.Handle("/blog.atom", middleware.Metrics("blog.atom", http.HandlerFunc(s.createAtom)))
	s.mux.Handle("/blog.json", middleware.Metrics("blog.json", http.HandlerFunc(s.createJSONFeed)))
	s.mux.Handle("/blog/", middleware.Metrics("blogpost", http.HandlerFunc(s.showPost)))
	s.mux.Handle("/blog/series", http.HandlerFunc(s.listSeries))
	s.mux.Handle("/blog/series/", http.HandlerFunc(s.showSeries))
	s.mux.Handle("/talks/", middleware.Metrics("talks", http.HandlerFunc(s.showTalk)))
	s.mux.Handle("/gallery/", middleware.Metrics("gallery", http.HandlerFunc(s.showGallery)))
	s.mux.Handle("/css/", http.FileServer(http.Dir(".")))
	s.mux.Handle("/static/", http.FileServer(http.Dir(".")))
	s.mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./static/js/sw.js")
	})
	s.mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./static/robots.txt")
	})
	s.mux.Handle("/sitemap.xml", middleware.Metrics("sitemap", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = smi.WriteTo(w)
	})))
	s.mux.HandleFunc("/api/pageview-timer", handlePageViewTimer)

	return s, nil
}

const icon = "https://alexheld.io/static/img/avatar.png"
