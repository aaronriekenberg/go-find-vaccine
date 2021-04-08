package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/kr/pretty"
	"github.com/umahmood/haversine"
)

type configuration struct {
	APIURLs                  []string `json:"api_urls"`
	AddUUIDParameter         bool     `json:"add_uuid_parameter"`
	SearchLatitude           float64  `json:"search_latitude"`
	SearchLongitude          float64  `json:"search_longitude"`
	NumNearestLocationsToLog int      `json:"num_nearest_locations_to_log"`
	FilterProvider           string   `json:"filter_provider"`
	FilterDistanceMiles      float64  `json:"filter_distance_miles"`
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
	URL                              string        `json:"url"`
	Provider                         string        `json:"provider"`
	ProviderLocationID               string        `json:"provider_location_id"`
	City                             string        `json:"city"`
	Name                             string        `json:"name"`
	State                            string        `json:"state"`
	Address                          string        `json:"address"`
	PostalCode                       string        `json:"postal_code"`
	AppointmentsLastFetched          string        `json:"appointments_last_fetched"`
	AppointmentsLastModified         string        `json:"appointments_last_modified"`
	AppointmentsAvailable            bool          `json:"appointments_available"`
	AppointmentsAvailableAllDoses    bool          `json:"appointments_available_all_doses"`
	AppointmentsAvailable2ndDoseOnly bool          `json:"appointments_available_2nd_dose_only"`
	Appointments                     []appointment `json:"appointments"`
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

	log.Printf("response.Header last-modified = %v", response.Header.Values("last-modified"))
	log.Printf("response.Header cf-cache-status = %v", response.Header.Values("cf-cache-status"))
	log.Printf("response.Header cf-ray = %v", response.Header.Values("cf-ray"))
	log.Printf("response.Header age = %v", response.Header.Values("age"))

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

	var apiResponses []*apiGETResponse

	for _, url := range configuration.APIURLs {
		if configuration.AddUUIDParameter {
			url = url + "?q=" + uuid.New().String()
		}
		apiResponse, err := makeAPIGETCall(url)
		if err != nil {
			log.Fatalf("makeAPIGETCall error %v", err)
		}

		log.Printf("got %v features in api response from %q", len(apiResponse.Features), url)

		apiResponses = append(apiResponses, apiResponse)
	}

	filterProvider := strings.ToLower(configuration.FilterProvider)
	log.Printf("filterProvider = %q", filterProvider)

	locationsWithAppointmentsPassingFilters := make([]vaccineLocationFeatureAndDistance, 0)

	for _, apiResponse := range apiResponses {

		for i := range apiResponse.Features {
			currentFeature := &(apiResponse.Features[i])

			// log.Printf("\nprocessing feature:\n%# v", pretty.Formatter(currentFeature))

			featureProvider := strings.TrimSpace(strings.ToLower(currentFeature.Properties.Provider))
			if (len(filterProvider) > 0) && (filterProvider != featureProvider) {
				continue
			}

			if (len(currentFeature.Properties.Appointments) == 0) && (!currentFeature.Properties.AppointmentsAvailable) {
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

			if (configuration.FilterDistanceMiles > 0) && (currentFeatureDistanceMiles > configuration.FilterDistanceMiles) {
				continue
			}

			vaccineLocationFeatureAndDistance := vaccineLocationFeatureAndDistance{
				vaccineLocationFeature: currentFeature,
				distanceMiles:          currentFeatureDistanceMiles,
			}

			locationsWithAppointmentsPassingFilters = append(locationsWithAppointmentsPassingFilters, vaccineLocationFeatureAndDistance)
		}
	}

	log.Printf("len(locationsWithAppointmentsPassingFilters) = %v", len(locationsWithAppointmentsPassingFilters))

	sort.Slice(locationsWithAppointmentsPassingFilters, func(i, j int) bool {
		return locationsWithAppointmentsPassingFilters[i].distanceMiles < locationsWithAppointmentsPassingFilters[j].distanceMiles
	})

	log.Printf("nearest %v features with appointments passing filters:", configuration.NumNearestLocationsToLog)

	for i := 0; (i < configuration.NumNearestLocationsToLog) && (i < len(locationsWithAppointmentsPassingFilters)); i = i + 1 {
		log.Printf("\navailable location:\n%# v", pretty.Formatter(locationsWithAppointmentsPassingFilters[i]))
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
