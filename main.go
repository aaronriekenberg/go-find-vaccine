package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"

	"github.com/kr/pretty"
	"github.com/umahmood/haversine"
)

type configuration struct {
	APIURL                   string  `json:"api_url"`
	SearchLatitude           float64 `json:"search_latitude"`
	SearchLongitude          float64 `json:"search_longitude"`
	NumNearestLocationsToLog int     `json:"num_nearest_locations_to_log"`
}

type geometry struct {
	Coordinates []float64 `json:"coordinates"`
}

type appointment struct {
	Time             string   `json:"time"`
	Type             string   `json:"type"`
	VaccineTypes     []string `json:"vaccine_types"`
	AppointmentTypes []string `json:"appointment_types"`
}

type vaccineLocationProperties struct {
	URL          string        `json:"url"`
	Provider     string        `json:"provider"`
	City         string        `json:"city"`
	Name         string        `json:"name"`
	State        string        `json:"state"`
	Address      string        `json:"address"`
	PostalCode   string        `json:"postal_code"`
	Appointments []appointment `json:"appointments"`
}

type vaccineLocationFeature struct {
	Geometry   geometry                  `json:"geometry"`
	Properties vaccineLocationProperties `json:"properties"`
}

type apiGETResponse struct {
	Features []vaccineLocationFeature `json:"features"`
}

type vaccineLocationFeatureAndDistance struct {
	vaccineLocationFeature *vaccineLocationFeature
	distanceMiles          float64
}

func makeHTTPGETCallWithResponse(url string, expectedStatusCode int) ([]byte, error) {
	const method = "GET"

	log.Printf("makeHTTPGETCallWithResponse url = %q", url)

	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		log.Printf("NewRequest error %v", err)
		return nil, err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Printf("error calling url %q method %v %v", url, method, err.Error())
		return nil, err
	}
	defer response.Body.Close()

	log.Printf("response.StatusCode = %v", response.StatusCode)
	if response.StatusCode != expectedStatusCode {
		return nil, fmt.Errorf("got unexpected http status code %v expected %v", response.StatusCode, expectedStatusCode)
	}

	responseBodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Printf("error reading response body %v", err.Error())
		return nil, err
	}

	return responseBodyBytes, nil
}

func makeAPIGETCall(url string) (*apiGETResponse, error) {
	log.Printf("makeAPIGETCall url = %q", url)

	responseBodyBytes, err := makeHTTPGETCallWithResponse(url, 200)
	if err != nil {
		log.Printf("makeHTTPGETCallWithResponse error %v", err.Error())
		return nil, err
	}

	var apiGETResponse apiGETResponse
	err = json.Unmarshal(responseBodyBytes, &apiGETResponse)
	if err != nil {
		log.Printf("error unmarshaling response body %v", err.Error())
		return nil, err
	}

	return &apiGETResponse, nil
}

func ReadConfiguration(configFile string) (*configuration, error) {
	log.Printf("reading config file %q", configFile)

	source, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var config configuration
	if err = json.Unmarshal(source, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func searchForAppointments(configuration *configuration) {
	log.Printf("begin searchForAppointments")

	searchLocation := haversine.Coord{
		Lat: configuration.SearchLatitude,
		Lon: configuration.SearchLongitude,
	}
	log.Printf("searchLocation:\n%# v", pretty.Formatter(searchLocation))

	apiResponse, err := makeAPIGETCall(configuration.APIURL)
	if err != nil {
		log.Fatalf("makeAPIGETCall error %v", err)
	}

	log.Printf("got %v features in api response", len(apiResponse.Features))

	locationsWithAppointments := make([]vaccineLocationFeatureAndDistance, 0)

	for i, _ := range apiResponse.Features {
		currentFeature := &(apiResponse.Features[i])

		// log.Printf("\nprocessing feature:\n%# v", pretty.Formatter(currentFeature))

		if len(currentFeature.Properties.Appointments) == 0 {
			// log.Printf("feature has no appointments")
			continue
		}

		if len(currentFeature.Geometry.Coordinates) != 2 {
			log.Printf("feature has unknown coordinates length %v", len(currentFeature.Geometry.Coordinates))
			continue
		}

		featureLocation := haversine.Coord{
			Lat: currentFeature.Geometry.Coordinates[1],
			Lon: currentFeature.Geometry.Coordinates[0],
		}
		// log.Printf("featureLocation:\n%# v", pretty.Formatter(featureLocation))

		currentFeatureDistanceMiles, _ := haversine.Distance(searchLocation, featureLocation)

		// log.Printf("currentFeatureDistanceMiles = %v", currentFeatureDistanceMiles)

		vaccineLocationFeatureAndDistance := vaccineLocationFeatureAndDistance{
			vaccineLocationFeature: currentFeature,
			distanceMiles:          currentFeatureDistanceMiles,
		}

		locationsWithAppointments = append(locationsWithAppointments, vaccineLocationFeatureAndDistance)
	}

	log.Printf("len(locationsWithAppointments) = %v", len(locationsWithAppointments))

	sort.Slice(locationsWithAppointments, func(i, j int) bool {
		return locationsWithAppointments[i].distanceMiles < locationsWithAppointments[j].distanceMiles
	})

	log.Printf("nearest %v features with appointments:", configuration.NumNearestLocationsToLog)

	for i := 0; (i < configuration.NumNearestLocationsToLog) && (i < len(locationsWithAppointments)); i = i + 1 {
		log.Printf("\nlocation:\n%# v", pretty.Formatter(locationsWithAppointments[i]))
	}
	log.Printf("end searchForAppointments")
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	if len(os.Args) != 2 {
		log.Fatalf("usage %v <config file>", os.Args[0])
	}

	configuration, err := ReadConfiguration(os.Args[1])
	if err != nil {
		log.Fatalf("error reading configuration %v", err)
	}

	log.Printf("configuration:\n%# v", pretty.Formatter(configuration))

	searchForAppointments(configuration)
}
