package main

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-ldap/ldap/v3"
	"github.com/vultr/govultr/v2"
	"github.com/rs/cors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
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
	accessKey    = os.Getenv("VULTR_ACCESS_KEY")
	secretKey    = os.Getenv("VULTR_SECRET_KEY")
)

type UserRequest struct {
	Username    string
	Password    string
	ImageData   []byte
}

func init() {
    s3Vultr = s3.New(session.Must(session.NewSession(&aws.Config{
        Region:      aws.String("sjc1"),
        Credentials: credentials.NewStaticCredentials(accessKey, secretKey, ""),
        Endpoint:    aws.String("https://sjc1.vultrobjects.com/"),
    })))
}

func ToPng(imageBytes []byte) ([]byte, error) {
	contentType := http.DetectContentType(imageBytes)

	switch contentType {
	case "image/png":
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

    // Configure AWS session with logging
    awsSession := session.Must(session.NewSession(&aws.Config{
        Credentials: credentials.NewStaticCredentials(accessKey, secretKey, ""),
        Region:      aws.String("sjc1"),
	Endpoint:    aws.String("https://sjc1.vultrobjects.com/"),
        LogLevel:    aws.LogLevel(aws.LogDebugWithHTTPBody),
        Logger:      aws.LoggerFunc(func(args ...interface{}) {
            fmt.Println(args...)
        }),
    }))
    // Create S3 service client
    s3Vultr := s3.New(awsSession)

    _, err := s3Vultr.PutObject(&s3.PutObjectInput{
        Body:   bytes.NewReader(imageData),
        Bucket: aws.String(bucketName),
        Key:    aws.String(objectKey),
	ACL:    aws.String("public-read"),
	ContentType: aws.String("image"),
    })
    if err != nil {
        return fmt.Errorf("Image upload failed: %v", err)
    }

    return nil
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Client connected:", r.RemoteAddr)
	fmt.Println("Received POST request to /upload")

	w.Header().Set("Access-Control-Allow-Origin", "https://www.ewnix.net")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		fmt.Println("Error parsing form:", err.Error())
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	imageFile, _, err := r.FormFile("image")
	if err != nil {
		fmt.Println("Error retrieving image file:", err.Error())
		http.Error(w, "Image file not provided", http.StatusBadRequest)
		return
	}
	defer imageFile.Close()

	l, err := ldap.Dial("tcp", fmt.Sprintf("%s:%s", ldapServer, ldapPort))
	if err != nil {
		fmt.Println("LDAP connection error:", err.Error())
		http.Error(w, "LDAP connection error", http.StatusInternalServerError)
		return
	}
	defer l.Close()

	userDN := fmt.Sprintf("cn=%s,%s", username, ldapBaseDN)
	err = l.Bind(userDN, password)
	if err != nil {
		fmt.Println("LDAP authentication failed:", err.Error())
		http.Error(w, "LDAP authentication failed", http.StatusUnauthorized)
		return
	}

	imageData, err := ioutil.ReadAll(imageFile)
	if err != nil {
		fmt.Println("Error reading image:", err.Error())
		http.Error(w, "Image reading failed", http.StatusBadRequest)
		return
	}

	pngData, err := ToPng(imageData)
	if err != nil {
		fmt.Println("Image conversion failed:", err.Error())
		http.Error(w, "Image conversion failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	usernameLower := strings.ToLower(username)

	err = uploadImageToStorage(usernameLower, pngData)
	if err != nil {
		fmt.Println("Image upload failed:", err.Error())
		http.Error(w, "Image upload failed", http.StatusInternalServerError)
		return
	}

	fmt.Println("Avatar uploaded!")
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

