package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/spatial-go/geoos/clusters"
	"github.com/spatial-go/geoos/clusters/dbscan"
	"github.com/spatial-go/geoos/space"
	"github.com/umahmood/haversine"
)

type Heatmap struct {
	Distance         float64 `json:"distance"` //from origin
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	EstimateLocation string  `json:"estimate_location"`
	Locality         string  `json:"locality"`
	PAXCount         int     `json:"pax_count"`
}

func getHeatMap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	var lat float64
	var long float64
	var err error
	latString := r.URL.Query().Get("lat")
	longString := r.URL.Query().Get("long")

	if latString != "" && longString != "" {
		lat, err = strconv.ParseFloat(latString, 64)
		if err != nil {
			encodeJSONError(w, err, http.StatusBadRequest)
			return
		}
		long, err = strconv.ParseFloat(longString, 64)
		if err != nil {
			encodeJSONError(w, err, http.StatusBadRequest)
			return
		}
	}

	now := time.Now()
	var dcurrent clusters.PointList
	var dpredict clusters.PointList
	// mock db
	for _, p := range plist {
		unixString, err := strconv.ParseInt(fmt.Sprintf("%.0f", p[2]), 10, 64)
		if err != nil {
			encodeJSONError(w, err, http.StatusBadRequest)
			return
		}
		pt := time.Unix(unixString, 0)
		then := now.Add(time.Duration(-2) * time.Hour)
		if then.After(pt) {
			dcurrent = append(dcurrent, space.Point{
				p[0],
				p[1],
			})
		} else {
			dpredict = append(dpredict, space.Point{
				p[0],
				p[1],
			})
		}
	}

	lhcurrent, err := cluster(lat, long, dcurrent)
	if err != nil {
		encodeJSONError(w, err, http.StatusBadRequest)
		return
	}

	lhpredict, err := cluster(lat, long, dpredict)
	if err != nil {
		encodeJSONError(w, err, http.StatusBadRequest)
		return
	}

	lh := make(map[string]interface{})
	lh["current"] = lhcurrent
	lh["predict"] = lhpredict

	encodeJSONResp(w, lh, http.StatusOK)
}

func cluster(lat float64, long float64, d clusters.PointList) (map[string][]Heatmap, error) {
	origin := haversine.Coord{Lat: lat, Lon: long}

	dbCluster, _ := dbscan.DBScan(d, 1.5, 10)

	var hlist []Heatmap
	for _, c := range dbCluster {
		var cl clusters.PointList
		var sumlat float64
		var sumlong float64
		for _, p := range c.Points {
			cl = append(cl, plist[p])
			sumlat += plist[p][0]
			sumlong += plist[p][1]
		}
		avglat := (float64(sumlat)) / (float64(len(cl)))
		avglong := (float64(sumlong)) / (float64(len(cl)))
		geocode, err := getGeocode(avglat, avglong)
		if err != nil {
			return nil, nil
		}
		var distance float64
		if lat != 0 && long != 0 {
			dest := haversine.Coord{Lat: avglat, Lon: avglong}
			_, km := haversine.Distance(origin, dest)
			distance = km
		}
		h := Heatmap{
			Distance:         distance,
			Latitude:         avglat,
			Longitude:        avglong,
			EstimateLocation: geocode.EstimateLocation,
			Locality:         geocode.Locality,
			PAXCount:         len(cl),
		}
		hlist = append(hlist, h)
	}

	sort.Slice(hlist, func(a, b int) bool {
		return hlist[a].Distance < hlist[b].Distance
	})

	lh := make(map[string][]Heatmap)

	for _, h := range hlist {
		val, ok := lh[h.Locality]
		if ok {
			newval := append(val, h)
			lh[h.Locality] = newval
		} else {
			lh[h.Locality] = []Heatmap{h}
		}
	}

	return lh, nil
}

type GmapsRes struct {
	Results []Result `json:"results"`
	Status  string   `json:"status"`
}

type Result struct {
	AddressComponents []struct {
		LongName  string   `json:"long_name"`
		ShortName string   `json:"short_name"`
		Types     []string `json:"types"`
	} `json:"address_components"`
	FormattedAddress string `json:"formatted_address"`
	Geometry         struct {
		Bounds struct {
			Northeast struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"northeast"`
			Southwest struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"southwest"`
		} `json:"bounds"`
		Location struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"location"`
		LocationType string `json:"location_type"`
		Viewport     struct {
			Northeast struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"northeast"`
			Southwest struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"southwest"`
		} `json:"viewport"`
	} `json:"geometry"`
	PlaceID string   `json:"place_id"`
	Types   []string `json:"types"`
}

type Geocode struct {
	EstimateLocation string `json:"estimate_location"`
	Locality         string `json:"locality"`
}

func getGeocode(lat float64, long float64) (Geocode, error) {
	requestURL := fmt.Sprintf(`https://maps.googleapis.com/maps/api/geocode/json?latlng=%v,%v&result_type=sublocality&key=asdf`, lat, long)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		fmt.Printf("client: could not create request: %s\n", err)
		os.Exit(1)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("client: error making http request: %s\n", err)
		os.Exit(1)
	}

	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
		os.Exit(1)
	}

	var gm GmapsRes

	if err = json.Unmarshal(resBody, &gm); err != nil {
		return Geocode{}, err
	}

	if gm.Status != "OK" {
		return getNearby(lat, long)
	}

	var g Geocode

	g.EstimateLocation = gm.Results[0].FormattedAddress

	ac := gm.Results[0].AddressComponents

	for _, a := range ac {
		if contains(a.Types, "locality") {
			g.Locality = a.LongName
			break
		}

	}

	return g, nil
}

type Nearby struct {
	HTMLAttributions []interface{} `json:"html_attributions"`
	NextPageToken    string        `json:"next_page_token"`
	Results          []struct {
		Geometry struct {
			Location struct {
				Lat float64 `json:"lat"`
				Lng float64 `json:"lng"`
			} `json:"location"`
			Viewport struct {
				Northeast struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				} `json:"northeast"`
				Southwest struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				} `json:"southwest"`
			} `json:"viewport"`
		} `json:"geometry"`
		Icon                string `json:"icon"`
		IconBackgroundColor string `json:"icon_background_color"`
		IconMaskBaseURI     string `json:"icon_mask_base_uri"`
		Name                string `json:"name"`
		Photos              []struct {
			Height           int      `json:"height"`
			HTMLAttributions []string `json:"html_attributions"`
			PhotoReference   string   `json:"photo_reference"`
			Width            int      `json:"width"`
		} `json:"photos,omitempty"`
		PlaceID        string   `json:"place_id"`
		Reference      string   `json:"reference"`
		Scope          string   `json:"scope"`
		Types          []string `json:"types"`
		Vicinity       string   `json:"vicinity"`
		BusinessStatus string   `json:"business_status,omitempty"`
		OpeningHours   struct {
			OpenNow bool `json:"open_now"`
		} `json:"opening_hours,omitempty"`
		PlusCode struct {
			CompoundCode string `json:"compound_code"`
			GlobalCode   string `json:"global_code"`
		} `json:"plus_code,omitempty"`
		Rating           float64 `json:"rating,omitempty"`
		UserRatingsTotal int     `json:"user_ratings_total,omitempty"`
	} `json:"results"`
	Status string `json:"status"`
}

func getNearby(lat float64, long float64) (Geocode, error) {
	requestURL := fmt.Sprintf("https://maps.googleapis.com/maps/api/place/nearbysearch/json?location=%v,%v&radius=100&key=asdf", lat, long)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		fmt.Printf("client: could not create request: %s\n", err)
		os.Exit(1)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("client: error making http request: %s\n", err)
		os.Exit(1)
	}

	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
		os.Exit(1)
	}

	var n Nearby

	if err = json.Unmarshal(resBody, &n); err != nil {
		return Geocode{}, err
	}

	if n.Status != "OK" {
		return Geocode{}, nil
	}

	var g Geocode

	for _, r := range n.Results {
		if contains(r.Types, "locality") {
			g.Locality = r.Name
			continue
		}
		g.EstimateLocation = r.Name
	}

	return g, nil
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
