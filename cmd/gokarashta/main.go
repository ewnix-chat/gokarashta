package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io/ioutil"
	"net/http"
	"os"
	"context"
	"os/exec"

	"github.com/nfnt/resize"
	"github.com/vultr/govultr/v2"
	"golang.org/x/oauth2"
	"github.com/go-ldap/ldap/v3"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/rs/cors"
	"github.com/gorilla/mux"
)

type RequestData struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Image    string `json:"image"`
}

var (
	apiKey       = os.Getenv("VULTR_API_KEY")
	vc           *govultr.Client
	ctx          = context.Background()
	s3Vultr      *s3.S3
	bucketName   = "ewnix-avatars"
	directory    = "users"
	filename     = "avatar.png"
	accessKey    = os.Getenv("ACCESS_KEY")
	secretKey    = os.Getenv("SECRET_KEY")
	sessionToken = os.Getenv("SESSION_TOKEN")
	ldapServer   = os.Getenv("LDAP_SERVER")
	ldapPort     = os.Getenv("LDAP_PORT")
	ldapBaseUserDN = os.Getenv("LDAP_BASE_USER_DN")
)

func init() {
    config := &oauth2.Config{}
    ts := config.TokenSource(ctx, &oauth2.Token{AccessToken: apiKey})
    vc = govultr.NewClient(oauth2.NewClient(ctx, ts))
}

func authenticateLDAP(username, password string) bool {
	l, err := ldap.Dial("tcp", fmt.Sprintf("%s:%s", ldapServer, ldapPort))
	if err != nil {
		return false
	}
	defer l.Close()

	err = l.Bind(fmt.Sprintf("cn=%s", username)+","+ldapBaseUserDN, password)
	return err == nil
}

func resizeImage(encodedImage string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(encodedImage)
	if err != nil {
		return nil, err
	}
	img, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		return nil, err
	}
	resized := resize.Resize(100, 0, img, resize.Lanczos3)
	buf := new(bytes.Buffer)
	err = jpeg.Encode(buf, resized, nil)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func convertToPNG(jpegData []byte) ([]byte, error) {
	cmd := exec.Command("convert", "-", "-format", "png", "-")
	cmd.Stdin = bytes.NewReader(jpegData)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func uploadObject(data []byte, username string) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fmt.Sprintf("%s/%s/%s", directory, username, filename)),
		Body:   bytes.NewReader(data),
	}
	_, err := s3Vultr.PutObject(input)
	return err
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	// Read request
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Decode JSON
	var requestData RequestData
	err = json.Unmarshal(body, &requestData)
	if err != nil {
		http.Error(w, "Failed to decode JSON", http.StatusBadRequest)
		return
	}

	// LDAP authentication
	if !authenticateLDAP(requestData.Username, requestData.Password) {
		http.Error(w, "Authentication failed", http.StatusUnauthorized)
		return
	}

	// Resize the image
	resizedImageData, format, err := resizeImage(requestData.Image)
	if err != nil {
		http.Error(w, "Image processing failed", http.StatusInternalServerError)
		return
	}

	var pngData []byte

	// Convert to PNG if not already in PNG format
	if format != "png" {
		pngData, err = convertToPNG(resizedImageData)
		if err != nil {
			http.Error(w, "Failed to convert image to PNG", http.StatusInternalServerError)
			return
		}
	} else {
		pngData = resizedImageData
	}

	// Upload the PNG image
	err = uploadObject(pngData, requestData.Username)
	if err != nil {
		http.Error(w, "Failed to upload image", http.StatusInternalServerError)
		return
	}

	w.Write([]byte("Processed"))
}


func main() {
	r := mux.NewRouter()

	// CORS setup
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"https://www.ewnix.net"},
		AllowCredentials: true,
		AllowedMethods:   []string{"POST"},
	})

	r.HandleFunc("/upload", handleRequest)

	// Apply CORS middleware
	handler := c.Handler(r)

	http.Handle("/", handler)
	http.ListenAndServe(":8080", nil)
}

