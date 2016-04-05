package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type weatherProvider interface {
	temperature(city string) (float64, error) // in Kelvin
}

type openWeatherMap struct{}
type weatherUnderground struct {
	apiKey string
}
type weatherData struct {
	Name string `json:"name"`
	Main struct {
		Kelvin float64 `json:"temp"`
	} `json:"main"`
}

func main() {

	http.HandleFunc("/", hello)
	http.HandleFunc("/weather/", weatherHandler)

	http.ListenAndServe(":8080", nil)
}

func weatherHandler(w http.ResponseWriter, r *http.Request) {
	begin := time.Now()
	city := strings.SplitN(r.URL.Path, "/", 3)[2]

	mw := multiWeatherProvider{
		openWeatherMap{},
		weatherUnderground{apiKey: "c1dc24acac1e0136"},
		weatherForecastIO{apiKey: "97f7a0df927e9db3f4782dcd586db617",
			gApi: googleApi{apiKey: "AIzaSyA51EGGq8bs8Bkk6S64AB1Z5CVsjDlvkMk"}},
	}

	temp, err := mw.temperature(city)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"city":   city,
		"temp":   temp,
		"took":   time.Since(begin).String(),
		"temp_c": temp - 273.15,
	})
}

func hello(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("hello!"))
}

func (w openWeatherMap) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.openweathermap.org/data/2.5/weather?APPID=d7a639d12a3886b86d61ef9931581644&q=" + city)
	if err != nil {
		return 0, err
	}

	defer resp.Body.Close()

	var d struct {
		Main struct {
			Kelvin float64 `json:"temp"`
		} `json:"main"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}

	log.Printf("openWeatherMap: %s: %.2f", city, d.Main.Kelvin)
	return d.Main.Kelvin, nil
}

func (w weatherUnderground) temperature(city string) (float64, error) {
	resp, err := http.Get("http://api.wunderground.com/api/" + w.apiKey + "/conditions/q/" + city + ".json")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var d struct {
		Observation struct {
			Celsius float64 `json:"temp_c"`
		} `json:"current_observation"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, err
	}
	kelvin := d.Observation.Celsius + 273.15
	log.Printf("WeatherUnderground: %s: %2.f", city, kelvin)
	return kelvin, nil
}

type weatherForecastIO struct {
	apiKey string
	gApi   googleApi
}

type googleApi struct {
	apiKey string
}

func (g googleApi) getCoordinates(city string) (float64, float64, error) {
	resp, err := http.Get("https://maps.googleapis.com/maps/api/geocode/json?address=" + city + "&key=" + g.apiKey)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	var d struct {
		Res []struct {
			Geometry struct {
				Location struct {
					Latitude  float64 `json:"lat"`
					Longitude float64 `json:"lng"`
				} `json:"location"`
			} `json:"geometry"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return 0, 0, err
	}

	lat := d.Res[0].Geometry.Location.Latitude
	lng := d.Res[0].Geometry.Location.Longitude

	log.Printf("googleApi: %s -> %.2f, %.2f",city, lat, lng)

	return lat, lng, nil
}

func (w weatherForecastIO) temperature(city string) (float64, error) {
	lat, lng, err := w.gApi.getCoordinates(city)
	if err != nil {
		return 0, err
	}

	request := "https://api.forecast.io/forecast/" + w.apiKey + "/" + FloatToString(lat) + "," + FloatToString(lng)
	resp, err := http.Get(request)
	if err != nil {
		return 0, err
	}

	var d struct {
		Currently struct {
			Fahrenheit float64 `json:"temperature"`
		} `json:"currently"`
	}

	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		log.Fatal(err)
		return 0, err
	}

	kelvin := (d.Currently.Fahrenheit + 459.67) * (5.0 / 9.0)
	log.Printf("weatherForecastIO: %s: %.2f", city, kelvin)
	return kelvin, nil
}

func FloatToString(input_num float64) string {
	// to convert a float number to a string
	return strconv.FormatFloat(input_num, 'f', 6, 64)
}

type multiWeatherProvider []weatherProvider

func (w multiWeatherProvider) temperature(city string) (float64, error) {
	temps := make(chan float64, len(w))
	errs := make(chan error, len(w))

	for _, provider := range w {
		go func(p weatherProvider) {
			k, err := p.temperature(city)
			if err != nil {
				errs <- err
				return
			}
			temps <- k
		}(provider)
	}

	sum := 0.0
	// Collect a temperature or an error from each provider.
	for i := 0; i < len(w); i++ {
		select {
		case temp := <-temps:
			sum += temp
		case err := <-errs:
			return 0, err
		}
	}

	return sum / float64(len(w)), nil
}
