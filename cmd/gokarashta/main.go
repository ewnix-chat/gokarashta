package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"os"

	"github.com/go-ldap/ldap/v3"
	"github.com/vultr/govultr/v2"
	"golang.org/x/oauth2"
	"github.com/rs/cors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
)

var (
	ldapServer   = os.Getenv("LDAP_SERVER")
	ldapPort     = os.Getenv("LDAP_PORT")
	ldapBaseDN   = os.Getenv("LDAP_BASE_USER_DN")
	apiKey       = os.Getenv("VULTR_API_KEY")
	bucketName   = "ewnix-avatars"
	avatarSuffix = "avatar.png"
	s3Vultr      *s3.S3
	vc           *govultr.Client
	ctx          = context.Background()
)

type UserRequest struct {
	Username    string
	Password    string
	Base64Image string
}

func init() {
	config := &oauth2.Config{}
	ts := config.TokenSource(ctx, &oauth2.Token{AccessToken: apiKey})
	vc = govultr.NewClient(oauth2.NewClient(ctx, ts))
}

func ToPng(imageBytes []byte) ([]byte, error) {
	contentType := http.DetectContentType(imageBytes)

	switch contentType {
	case "image/png":
		// No need to convert, it's already a PNG
		return imageBytes, nil
	case "image/jpeg":
		img, err := jpeg.Decode(bytes.NewReader(imageBytes))
		if err != nil {
			return nil, fmt.Errorf("unable to decode jpeg: %v", err)
		}

		buf := new(bytes.Buffer)
		if err := png.Encode(buf, img); err != nil {
			return nil, fmt.Errorf("unable to encode png: %v", err)
		}

		return buf.Bytes(), nil
	}

	return nil, fmt.Errorf("unable to convert %#v to png", contentType)
}

func uploadImageToStorage(username string, imageData []byte) error {
	objectKey := username + "/" + avatarSuffix

	_, err := s3Vultr.PutObject(&s3.PutObjectInput{
		Body:   bytes.NewReader(imageData),
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return fmt.Errorf("Image upload failed: %v", err)
	}

	return nil
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	var userReq UserRequest

	r.ParseMultipartForm(10 << 20)
	userReq.Username = r.FormValue("username")
	userReq.Password = r.FormValue("password")
	userReq.Base64Image = r.FormValue("image")

	l, err := ldap.Dial("tcp", fmt.Sprintf("%s:%s", ldapServer, ldapPort))
	if err != nil {
		http.Error(w, "LDAP connection error", http.StatusInternalServerError)
		return
	}
	defer l.Close()

	userDN := fmt.Sprintf("cn=%s,%s", userReq.Username, ldapBaseDN)
	err = l.Bind(userDN, userReq.Password)
	if err != nil {
		http.Error(w, "LDAP authentication failed", http.StatusUnauthorized)
		return
	}

	imageData, err := base64.StdEncoding.DecodeString(userReq.Base64Image)
	if err != nil {
		http.Error(w, "Image decoding failed", http.StatusBadRequest)
		return
	}

	pngData, err := ToPng(imageData)
	if err != nil {
		http.Error(w, "Image conversion failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = uploadImageToStorage(userReq.Username, pngData)
	if err != nil {
		http.Error(w, "Image upload failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Image uploaded successfully"))
}

func main() {
	corsOptions := cors.New(cors.Options{
		AllowedOrigins: []string{"https://www.ewnix.net"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/upload", handleUpload)

	handler := corsOptions.Handler(mux)

	log.Fatal(http.ListenAndServe(":8080", handler))
}

