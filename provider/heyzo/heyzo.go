package heyzo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"
	"golang.org/x/net/html"
	"golang.org/x/text/language"

	"github.com/metatube-community/metatube-sdk-go/common/parser"
	"github.com/metatube-community/metatube-sdk-go/model"
	"github.com/metatube-community/metatube-sdk-go/provider"
	"github.com/metatube-community/metatube-sdk-go/provider/internal/scraper"
)

var (
	_ provider.MovieProvider = (*Heyzo)(nil)
	_ provider.MovieReviewer = (*Heyzo)(nil)
)

const (
	Name     = "HEYZO"
	Priority = 1000
)

const (
	baseURL   = "https://www.heyzo.com/"
	movieURL  = "https://www.heyzo.com/movie/%s"
	reviewURL = "https://www.heyzo.com/app_v2/review_getjs/?id=%s&showall=1&r=%f&lang=%s"
)

type Heyzo struct {
	*scraper.Scraper
}

func New() *Heyzo {
	return &Heyzo{scraper.NewDefaultScraper(
		Name, baseURL, Priority,
		language.Japanese,
		// heyzo.com is protected by Cloudflare bot management which returns
		// 404 for requests lacking a browser-like `Accept` header.
		// Go's net/http does not send `Accept` by default, and colly falls
		// back to `Accept: */*`, which Cloudflare rejects.
		scraper.WithHeaders(map[string]string{
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Accept-Language": "ja-JP,ja;q=0.9,en;q=0.8",
			"Referer":         "https://www.heyzo.com/",
		}),
	)}
}

func (hzo *Heyzo) GetMovieReviewsByID(id string) (reviews []*model.MovieReviewDetail, err error) {
	c := hzo.ClonedCollector()

	var movieSeq string
	c.OnHTML(`script`, func(e *colly.HTMLElement) {
		if movieSeq != "" {
			return // already found
		}
		text := e.Text
		// Look for movie_seq in the Next.js flight data.
		seqRe := regexp.MustCompile(`movie_seq[\\]?:[\\]?"?(\d+)`)
		seqMatch := seqRe.FindStringSubmatch(text)
		if len(seqMatch) != 2 {
			return
		}
		movieSeq = seqMatch[1]
	})

	if vErr := c.Visit(fmt.Sprintf(movieURL, id)); vErr != nil {
		err = vErr
		return
	}

	if movieSeq == "" {
		return
	}

	// Fetch reviews via the legacy API.
	d := c.Clone()
	d.OnResponse(func(r *colly.Response) {
		data := struct {
			Comments []struct {
				Username string `json:"user_name"`
				Date     string `json:"date"`
				Comment  string `json:"comment"`
				Score    struct {
					Overall string `json:"overall"`
				} `json:"score"`
			} `json:"comments"`
		}{}
		if json.Unmarshal(r.Body, &data) == nil {
			for _, row := range data.Comments {
				if row.Username == "" || row.Comment == "" {
					continue
				}
				reviews = append(reviews, &model.MovieReviewDetail{
					Author:  row.Username,
					Comment: row.Comment,
					Score:   parser.ParseScore(row.Score.Overall),
					Date:    parser.ParseDate(row.Date),
				})
			}
		}
	})

	ru := fmt.Sprintf(reviewURL, movieSeq, 0.0, "ja")
	err = d.Visit(ru)
	return
}

func (hzo *Heyzo) GetMovieReviewsByURL(rawURL string) (reviews []*model.MovieReviewDetail, err error) {
	id, err := hzo.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}
	return hzo.GetMovieReviewsByID(id)
}

func (hzo *Heyzo) NormalizeMovieID(id string) string {
	if ss := regexp.MustCompile(`^(?i)(?:heyzo[-_])?(\d+)$`).FindStringSubmatch(id); len(ss) == 2 {
		return ss[1]
	}
	return ""
}

func (hzo *Heyzo) GetMovieInfoByID(id string) (info *model.MovieInfo, err error) {
	return hzo.GetMovieInfoByURL(fmt.Sprintf(movieURL, id))
}

func (hzo *Heyzo) ParseMovieIDFromURL(rawURL string) (string, error) {
	homepage, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	// New URL format: /movie/{id}
	// Old URL format: /moviepages/{id}/index.html
	segments := strings.Split(strings.Trim(homepage.Path, "/"), "/")
	if len(segments) >= 2 && segments[0] == "movie" {
		return segments[1], nil
	}
	// Fallback to old behavior: last path segment's parent dir
	return path.Base(path.Dir(homepage.Path)), nil
}

func (hzo *Heyzo) GetMovieInfoByURL(rawURL string) (info *model.MovieInfo, err error) {
	id, err := hzo.ParseMovieIDFromURL(rawURL)
	if err != nil {
		return
	}

	info = &model.MovieInfo{
		ID:            id,
		Number:        fmt.Sprintf("HEYZO-%s", id),
		Provider:      hzo.Name(),
		Homepage:      rawURL,
		Maker:         "HEYZO",
		Actors:        []string{},
		PreviewImages: []string{},
		Genres:        []string{},
	}

	c := hzo.ClonedCollector()

	// Title — the new Next.js site uses h1 inside the movieInfo div.
	c.OnHTML(`div[class*="Movie_movieInfo"] h1`, func(e *colly.HTMLElement) {
		if info.Title == "" {
			info.Title = strings.TrimSpace(e.Text)
		}
	})

	// Summary — the new site uses a p tag with class containing "Movie_memo".
	c.OnHTML(`p[class*="Movie_memo"]`, func(e *colly.HTMLElement) {
		if info.Summary == "" {
			info.Summary = strings.TrimSpace(e.Text)
		}
	})

	// Cover + Thumb — og:image meta tag is still present in the new site.
	c.OnHTML(`meta[property="og:image"]`, func(e *colly.HTMLElement) {
		if info.CoverURL == "" {
			info.CoverURL = e.Request.AbsoluteURL(e.Attr("content"))
			info.ThumbURL = info.CoverURL
		}
	})

	// Preview Video — og:video meta tag contains the HLS sample URL.
	c.OnHTML(`meta[property="og:video"]`, func(e *colly.HTMLElement) {
		if info.PreviewVideoHLSURL == "" {
			info.PreviewVideoHLSURL = e.Request.AbsoluteURL(e.Attr("content"))
		}
	})

	// Fields — the new site uses div.Movie_item with span labels.
	// Pattern: <div class="Movie_item..."><span>出演</span><span><a href="...">Actor</a></span></div>
	//          <div class="Movie_item..."><span>配信日</span>2026年11月8日</div>
	c.OnHTML(`div[class*="Movie_item"]`, func(e *colly.HTMLElement) {
		// Use goquery DOM API for CSS selector based child traversal.
		spans := e.DOM.Find("span")
		if spans.Length() == 0 {
			return
		}
		label := strings.TrimSpace(spans.First().Text())
		switch label {
		case "出演":
			// Actor links inside the second span.
			spans.Eq(1).Find("a").Each(func(_ int, s *goquery.Selection) {
				name := strings.TrimSpace(s.Text())
				if name != "" {
					info.Actors = append(info.Actors, name)
				}
			})
		case "配信日":
			// Release date text is in the second span (or directly after first span).
			// Format: 2026年11月8日
			var dateText string
			if spans.Length() > 1 {
				dateText = strings.TrimSpace(spans.Eq(1).Text())
			}
			if dateText == "" {
				// Text may be a direct child node after the span.
				dateText = strings.TrimSpace(e.DOM.Contents().FilterFunction(func(_ int, s *goquery.Selection) bool {
					return s.Get(0).Type == html.TextNode
				}).Text())
			}
			info.ReleaseDate = parser.ParseDate(dateText)
		case "シリーズ":
			if spans.Length() > 1 {
				info.Series = strings.TrimSpace(spans.Eq(1).Text())
				if info.Series == "-" || info.Series == "−" {
					info.Series = ""
				}
			}
		case "評価":
			if spans.Length() > 1 {
				info.Score = parser.ParseScore(spans.Eq(1).Text())
			}
		}
	})

	// Genres — the new site uses div.Movie_tags with <a> links.
	c.OnHTML(`div[class*="Movie_tags"]`, func(e *colly.HTMLElement) {
		e.DOM.Find("a").Each(func(_ int, s *goquery.Selection) {
			genre := strings.TrimSpace(s.Text())
			if genre != "" {
				info.Genres = append(info.Genres, genre)
			}
		})
	})

	// Preview Images (gallery) — the new site uses Next.js Image component
	// with URL-encoded source images in srcSet attributes.
	c.OnHTML(`div[class*="Movie_galleryContainer"] img`, func(e *colly.HTMLElement) {
		// Extract the original image URL from the Next.js image proxy URL.
		// Format: /_next/image?url=https%3A%2F%2Fwww.heyzo.com%2Fcontents%2F3000%2F3977%2Fgallery%2Fthumbnail_001.jpg&w=256&q=75
		srcSet, _ := e.DOM.Attr("srcset")
		if srcSet == "" {
			srcSet, _ = e.DOM.Attr("src")
		}
		// Find URL-encoded content paths
		re := regexp.MustCompile(`url=(https?%3A%2F%2F[^&\s]+)`)
		matches := re.FindAllStringSubmatch(srcSet, -1)
		for _, m := range matches {
			if len(m) == 2 {
				decoded, derr := url.QueryUnescape(m[1])
				if derr == nil && decoded != "" {
					info.PreviewImages = append(info.PreviewImages, decoded)
				}
			}
		}
	})

	// Runtime & movie data — extract from Next.js flight data in script tags.
	// The new site embeds movie info in serialized RSC payloads.
	c.OnHTML(`script`, func(e *colly.HTMLElement) {
		text := e.Text

		// Duration: "duration\":\"00:55:12"
		if info.Runtime == 0 {
			durRe := regexp.MustCompile(`duration[\\]?:[\\]?"(\d{2}:\d{2}:\d{2})`)
			if m := durRe.FindStringSubmatch(text); len(m) == 2 {
				info.Runtime = parser.ParseRuntime(m[1])
			}
		}

		// Movie seq — used for review API
		// (already handled in review function)

		// Preview images from capture URLs in Next.js data (seek bar thumbnails)
		// Format: https://www.heyzo.com/contents/3000/3977/images/capture1.jpg
		// These are added in addition to the gallery images above.
		captureRe := regexp.MustCompile(`https://www\.heyzo\.com/contents/\d+/` + regexp.QuoteMeta(id) + `/images/capture\d+\.jpg`)
		matches := captureRe.FindAllString(text, -1)
		seen := map[string]bool{}
		for _, img := range info.PreviewImages {
			seen[img] = true
		}
		for _, img := range matches {
			img = strings.TrimRight(img, "\\")
			if !seen[img] {
				seen[img] = true
				info.PreviewImages = append(info.PreviewImages, img)
			}
		}
	})

	err = c.Visit(info.Homepage)
	return
}

func init() {
	provider.Register(Name, New)
}
