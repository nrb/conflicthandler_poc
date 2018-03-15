package main

import (
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"os"
	"strings"
)

func resetMetadataAndStatus(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	metadata, err := GetMap(obj.UnstructuredContent(), "metadata")
	if err != nil {
		return nil, err
	}

	for k := range metadata {
		switch k {
		case "name", "namespace", "labels", "annotations":
		default:
			delete(metadata, k)
		}
	}

	// this should never be backed up anyway, but remove it just
	// in case.
	delete(obj.UnstructuredContent(), "status")

	return obj, nil
}

// GetValue returns the object at root[path], where path is a dot separated string.
func GetValue(root map[string]interface{}, path string) (interface{}, error) {
	if root == nil {
		return "", errors.New("root is nil")
	}

	pathParts := strings.Split(path, ".")
	key := pathParts[0]

	obj, found := root[pathParts[0]]
	if !found {
		return "", errors.Errorf("key %v not found", pathParts[0])
	}

	if len(pathParts) == 1 {
		return obj, nil
	}

	subMap, ok := obj.(map[string]interface{})
	if !ok {
		return "", errors.Errorf("value at key %v is not a map[string]interface{}", key)
	}

	return GetValue(subMap, strings.Join(pathParts[1:], "."))
}

// GetMap returns the map at root[path], where path is a dot separated string.
func GetMap(root map[string]interface{}, path string) (map[string]interface{}, error) {
	obj, err := GetValue(root, path)
	if err != nil {
		return nil, err
	}

	ret, ok := obj.(map[string]interface{})
	if !ok {
		return nil, errors.Errorf("value at path %v is not a map[string]interface{}", path)
	}

	return ret, nil
}

// GetSlice returns the slice at root[path], where path is a dot separated string.
func GetSlice(root map[string]interface{}, path string) ([]interface{}, error) {
	obj, err := GetValue(root, path)
	if err != nil {
		return nil, err
	}

	ret, ok := obj.([]interface{})
	if !ok {
		return nil, errors.Errorf("value at path %v is not a []interface{}", path)
	}

	return ret, nil
}

// GetString returns the string at root[path], where path is a dot separated string.
func GetString(root map[string]interface{}, path string) (string, error) {
	obj, err := GetValue(root, path)
	if err != nil {
		return "", err
	}

	str, ok := obj.(string)
	if !ok {
		return "", errors.Errorf("value at path %v is not a string", path)
	}

	return str, nil
}

func printObj(name string, obj interface{}) {
	fmt.Println()
	fmt.Println(name, ":")
	fmt.Println("   ", obj)
	fmt.Println()
}

func processServiceAccounts() {
	inclusterRaw, err := ioutil.ReadFile("./incluster.json")
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	var incluster *unstructured.Unstructured
	json.Unmarshal(inclusterRaw, &incluster)
	incluster, _ = resetMetadataAndStatus(incluster)

	backupRaw, err := ioutil.ReadFile("./backup.json")
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	var backup *unstructured.Unstructured
	json.Unmarshal(backupRaw, &backup)

	// Lives in the SA plugin
	backup, _ = resetMetadataAndStatus(backup)
	secrets, _ := GetSlice(backup.UnstructuredContent(), "secrets")
	for i := len(secrets) - 1; i >= 0; i-- {
		secret := secrets[i]
		msg := fmt.Sprintf("Secret is: %v", secret)
		fmt.Println(msg)
		val, _ := GetString(secret.(map[string]interface{}), "name")
		if strings.HasPrefix(val, "default-token-") {
			fmt.Println("Found default-token-")
			secrets = append(secrets[:i], secrets[i+1:]...)
		}
	}
	// Attach the secrets so we can do our diff
	backup.Object["secrets"] = secrets

	areEqual := equality.Semantic.DeepEqual(incluster, backup)

	fmt.Println("In-cluster and backup are equal: ", areEqual)

	var desired *unstructured.Unstructured
	if !areEqual {
		desired = incluster.DeepCopy()
		desired.Object["imagePullSecrets"] = backup.Object["imagePullSecrets"]
		if len(secrets) != 0 {
			clusterSecrets, _ := GetSlice(desired.UnstructuredContent(), "secrets")
			clusterSecrets = append(clusterSecrets, secrets...)
			desired.Object["secrets"] = clusterSecrets
		}
	}

	fmt.Println("\033[H\033[2J")
	printObj("incluster", incluster)
	printObj("backup", backup)
	printObj("incluster", incluster)
	desiredRaw, _ := json.Marshal(desired)

	origThreeWayMergePatch, err := jsonmergepatch.CreateThreeWayJSONMergePatch(inclusterRaw, desiredRaw, nil)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	printObj("jsonmergepatch.CreateThreeWayJSONMergePatch(inclusterRaw, desiredRaw, nil) output:", string(origThreeWayMergePatch))
	fmt.Println("\n")
}

func main() {
	processServiceAccounts()
}
