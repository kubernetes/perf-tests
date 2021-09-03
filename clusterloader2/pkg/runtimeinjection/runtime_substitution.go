package runtimeinjection

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// This function substitutes all runtime injections:
// * marshalls into a string
// * searches for runtime injections
// * for each runtime injection, calls the function and replaces the string with the result
// * unmarshals back into an object
func SubstituteRuntimeInjections(obj *unstructured.Unstructured) error {
	objBytes, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("Error marshalling in runtimeinjection: %v", err)
	}
	objString := string(objBytes)
	// First group is the injection type, second group will be the args
	re := regexp.MustCompile("\"RuntimeInjection.([a-zA-Z]+)\\((.+)\\)\"")
	// results: [[<full-match>, <first-string>, <second-string>]]
	results := re.FindAllStringSubmatch(objString, -1)
	// results = [][]string{}
	for _, result := range results {
		if len(result) != 3 {
			return fmt.Errorf("Invalid RuntimeInjection")
		}

		fullMatch := result[0]
		injectionType := result[1]
		injectionArgs := result[2]
		switch injectionType {
		case "RandomInt":
			max, err := strconv.Atoi(injectionArgs)
			if err != nil {
				return fmt.Errorf("Unsupported RuntimeInjection arg: %v", injectionArgs)
			}
			randomInteger := rand.Intn(max)
			objString = strings.Replace(objString, fullMatch, fmt.Sprint(randomInteger), 1)
		default:
			return fmt.Errorf("Unsupported RuntimeInjection: %v", injectionType)
		}
	}

	fmt.Println(objString)
	err = obj.UnmarshalJSON([]byte(objString))
	if err != nil {
		return fmt.Errorf("Error unmarshalling in runtimeinjection: %v", err)
	}
	return nil
}
