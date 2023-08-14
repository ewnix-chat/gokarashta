package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
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
	vultrAPIKey  = os.Getenv("VULTR_API_KEY")
	bucketName   = "ewnix-avatars"
	avatarSuffix = "avatar.png"
	s3Vultr *s3.S3
)

type UserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Base64Image string `json:"image"`
}

func convertToPNG(imageData []byte) ([]byte, error) {
	imgFormat := http.DetectContentType(imageData)

	if imgFormat == "image/png" {
		// Image is already in PNG format, no conversion needed
		return imageData, nil
	} else if imgFormat == "image/jpeg" || imgFormat == "image/jpg" {
		img, _, err := image.Decode(bytes.NewReader(imageData))
		if err != nil {
			return nil, fmt.Errorf("JPEG decoding failed: %v", err)
		}

		var pngBuf bytes.Buffer
		err = png.Encode(&pngBuf, img)
		if err != nil {
			return nil, fmt.Errorf("PNG encoding failed: %v", err)
		}

		return pngBuf.Bytes(), nil
	} else {
		return nil, fmt.Errorf("Unsupported image format: %s", imgFormat)
	}
}

func uploadImageToStorage(username string, imageData []byte) error {
	config := &oauth2.Config{}
	ts := config.TokenSource(context.Background(), &oauth2.Token{AccessToken: vultrAPIKey})
	vc := govultr.NewClient(oauth2.NewClient(context.Background(), ts))

	objectKey := username + "/" + avatarSuffix

	// Upload the image using s3Vultr.PutObject
	_, err := s3Vultr.PutObject(&s3.PutObjectInput{
		Body:   bytes.NewReader(imageData),
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
		// Set any other required parameters (ACL, ContentType, etc.)
	})
	if err != nil {
		return fmt.Errorf("Image upload failed: %v", err)
	}

	log.Printf("Image for user %q uploaded successfully!", username)
	return nil
}


func handleUpload(w http.ResponseWriter, r *http.Request) {
	var userReq UserRequest

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&userReq); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

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

	pngData, err := convertToPNG(imageData)
	if err != nil {
		http.Error(w, "Image conversion failed", http.StatusInternalServerError)
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
	// Set up CORS middleware
	corsOptions := cors.New(cors.Options{
		AllowedOrigins: []string{"https://www.ewnix.net"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	})

	// Create a mux with CORS middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", handleUpload)

	// Use CORS middleware with your handler
	handler := corsOptions.Handler(mux)

	log.Fatal(http.ListenAndServe(":8080", handler))
}

