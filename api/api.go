package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /package/{package}", invalidPackageHandler)
	mux.HandleFunc("GET /package/{package}/{version}", packageHandler)
	return mux
}

type npmPackageMetaResponse struct {
	Versions map[string]npmPackageResponse `json:"versions"`
}

type npmPackageResponse struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

type NpmPackageVersion struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

func packageHandler(w http.ResponseWriter, r *http.Request) {

	log.Printf("handling get task at %s\n", r.URL.Path)

	pkgName := r.PathValue("package")
	pkgVersion := r.PathValue("version")

	if pkgName == "" || pkgVersion == "" {
		http.Error(w, "Invalid request path. Expected format: /package/{name}/{version}", http.StatusBadRequest)
		return
	}

	// Validate the version format
	if !isValidVersion(pkgVersion) {
		http.Error(w, fmt.Sprintf("Invalid version format: %s.", pkgVersion), http.StatusBadRequest)
		return
	}

	pkgMeta, err := fetchPackageMeta(pkgName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch package metadata: %v", err), http.StatusInternalServerError)
		return
	}

	concreteVersion, err := highestCompatibleVersion(pkgVersion, pkgMeta)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to find compatible version: %v", err), http.StatusInternalServerError)
		return
	}

	rootPkg := &NpmPackageVersion{Name: pkgName, Version: concreteVersion, Dependencies: map[string]string{}}

	npmPkg, err := fetchPackage(rootPkg.Name, rootPkg.Version)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch package: %v", err), http.StatusInternalServerError)
		return
	}

	for dependencyName, dependencyVersionConstraint := range npmPkg.Dependencies {
		pkgMeta, err := fetchPackageMeta(dependencyName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch metadata for dependency %s: %v", dependencyName, err), http.StatusInternalServerError)
			return
		}

		concreteVersion, err := highestCompatibleVersion(dependencyVersionConstraint, pkgMeta)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to find compatible version for dependency %s: %v", dependencyName, err), http.StatusInternalServerError)
			return
		}

		rootPkg.Dependencies[dependencyName] = concreteVersion
	}

	stringified, err := json.MarshalIndent(rootPkg, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(stringified)
}

func highestCompatibleVersion(constraintStr string, versions *npmPackageMetaResponse) (string, error) {
	// Parse the version constraint
	constraint, err := semver.NewConstraint(constraintStr)
	if err != nil {
		return "", fmt.Errorf("invalid constraint: %w", err)
		//return "", err
	}

	// Filter versions based on the constraint
	filtered := filterCompatibleVersions(constraint, versions)

	// Sort filtered versions in ascending order
	sort.Sort(sort.Reverse(filtered))

	// Check if there are any compatible versions
	if len(filtered) == 0 {
		return "", errors.New("no compatible versions found")
	}

	// Return the highest compatible version
	return filtered[0].String(), nil
}

func filterCompatibleVersions(constraint *semver.Constraints, pkgMeta *npmPackageMetaResponse) semver.Collection {
	var compatible semver.Collection
	for version := range pkgMeta.Versions {
		semVer, err := semver.NewVersion(version)
		if err != nil {
			continue
		}
		if constraint.Check(semVer) {
			compatible = append(compatible, semVer)
		}
	}
	return compatible
}

func fetchPackage(name, version string) (*npmPackageResponse, error) {
	resp, err := http.Get(fmt.Sprintf("https://registry.npmjs.org/%s/%s", name, version))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

func fetchPackageMeta(p string) (*npmPackageMetaResponse, error) {
	resp, err := http.Get(fmt.Sprintf("https://registry.npmjs.org/%s", p))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed npmPackageMetaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}

// isValidVersion checks if the provided version string is a valid semantic version.
func isValidVersion(version string) bool {

	version = strings.Trim(version, " #?/") // Adjust as needed

	_, err := semver.NewVersion(version)
	if err != nil {
		return false
	}

	// Custom validation: Ensure the version string contains at least two dots
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return false
	}

	return true
}

func invalidPackageHandler(w http.ResponseWriter, r *http.Request) {

	log.Printf("invalid get task at %s\n", r.URL.Path)
	http.Error(w, "Invalid request path. Expected format: /package/{name}/{version}", http.StatusBadRequest)
}
