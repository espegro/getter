package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/nfnt/resize" // for JPEG decoding
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var validFilename = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

func isValidJPEG(data io.Reader) bool {
	_, err := jpeg.Decode(data)
	return err == nil
}

func scaledHandler(saveDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get the filename from the query
		filename := r.URL.Query().Get("filename")
		if filename == "" || !validFilename.MatchString(filename) {
			http.Error(w, "Invalid or missing filename", http.StatusBadRequest)
			return
		}

		// Open the image file
		imgFile, err := os.Open(fmt.Sprintf("%s/%s.jpg", saveDir, filename))
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer imgFile.Close()

		// Get the file info
		fileInfo, err := imgFile.Stat()
		if err != nil {
			http.Error(w, "Error getting file info", http.StatusInternalServerError)
			return
		}

		// Decode the image
		img, _, err := image.Decode(imgFile)
		if err != nil {
			http.Error(w, "Error decoding the image", http.StatusInternalServerError)
			return
		}

		// Get the box parameters from the query
		x1Str, y1Str, x2Str, y2Str := r.URL.Query().Get("x1"), r.URL.Query().Get("y1"), r.URL.Query().Get("x2"), r.URL.Query().Get("y2")
		x1, y1, x2, y2 := 0, 0, img.Bounds().Max.X, img.Bounds().Max.Y
		if x1Str != "" {
			x1, err = strconv.Atoi(x1Str)
			if err != nil {
				http.Error(w, "Invalid x1 parameter", http.StatusBadRequest)
				return
			}
		}
		if y1Str != "" {
			y1, err = strconv.Atoi(y1Str)
			if err != nil {
				http.Error(w, "Invalid y1 parameter", http.StatusBadRequest)
				return
			}
		}
		if x2Str != "" {
			x2, err = strconv.Atoi(x2Str)
			if err != nil {
				http.Error(w, "Invalid x2 parameter", http.StatusBadRequest)
				return
			}
		}
		if y2Str != "" {
			y2, err = strconv.Atoi(y2Str)
			if err != nil {
				http.Error(w, "Invalid y2 parameter", http.StatusBadRequest)
				return
			}
		}

		// Crop the image
		subImg := img.(interface {
			SubImage(r image.Rectangle) image.Image
		}).SubImage(image.Rect(x1, y1, x2, y2))

		// Get the scale width from the query
		wStr := r.URL.Query().Get("w")
		if wStr != "" {
			width, err := strconv.ParseUint(wStr, 10, 32)
			if err != nil {
				http.Error(w, "Invalid w parameter", http.StatusBadRequest)
				return
			}
			scaleFactor := float64(width) / float64(subImg.Bounds().Dx())
			newHeight := uint(float64(subImg.Bounds().Dy()) * scaleFactor)
			subImg = resize.Resize(uint(width), newHeight, subImg, resize.Lanczos3)
		}

		// Convert the subImg to RGBA
		rgbaImg := image.NewRGBA(subImg.Bounds())
		draw.Draw(rgbaImg, rgbaImg.Bounds(), subImg, image.Point{}, draw.Src)

		// Draw the time and date if nolabel is not set
		nolabel := r.URL.Query().Get("nolabel")
		if nolabel == "" {
			fileModTime := fileInfo.ModTime().Format("2006-01-02 15:04:05")
			currentTime := time.Now().Format("2006-01-02 15:04:05")
			label := fmt.Sprintf("FileTime: %s CurrentTime: %s", fileModTime, currentTime)
			id := r.URL.Query().Get("id")
			if id != "" {
				ip := r.Header.Get("X-Forwarded-For")
				if ip == "" {
					ip = r.RemoteAddr
				} else {
					ips := strings.Split(ip, ", ")
					ip = ips[0]
				}
				userAgent := r.UserAgent()
				label = fmt.Sprintf("%s IP: %s User-Agent: %s", label, ip, userAgent)
			}

			// Get the color parameter
			colorParam := r.URL.Query().Get("color")
			col := color.RGBA{255, 255, 255, 255} // Default color is white
			if colorParam != "" {
				colHex, err := strconv.ParseUint(colorParam, 16, 32)
				if err == nil {
					col = color.RGBA{
						R: uint8(colHex >> 16),
						G: uint8((colHex >> 8) & 0xFF),
						B: uint8(colHex & 0xFF),
						A: 255,
					}
				}
			}

			drawText(rgbaImg, label, 10, 20, col)
		}

		// Encode the image with watermark to JPEG
		buffer := new(bytes.Buffer)
		err = jpeg.Encode(buffer, rgbaImg, nil)
		if err != nil {
			http.Error(w, "Error encoding the cropped image", http.StatusInternalServerError)
			return
		}

		// Set the response header and write the image data
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(buffer.Bytes())
	}
}

func saveHandler(saveDir string, maxSize int64, bearerToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for the correct request method
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check for bearer token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || authHeader != "Bearer "+bearerToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Check the filename
		filename := r.URL.Query().Get("filename")
		if filename == "" || !validFilename.MatchString(filename) {
			http.Error(w, "Invalid or missing filename", http.StatusBadRequest)
			return
		}

		uploadedFile, _, err := r.FormFile("image")
		if err != nil {
			http.Error(w, "Error reading uploaded file", http.StatusBadRequest)
			return
		}
		defer uploadedFile.Close()

		data, err := io.ReadAll(io.LimitReader(uploadedFile, maxSize))
		if err != nil {
			http.Error(w, "Error reading file data", http.StatusInternalServerError)
			return
		}

		if !isValidJPEG(bytes.NewReader(data)) {
			http.Error(w, "Uploaded file is not a valid JPEG", http.StatusBadRequest)
			return
		}

		outFilePath := fmt.Sprintf("%s/%s.jpg", saveDir, filename)
		outFile, err := os.Create(outFilePath)
		if err != nil {
			http.Error(w, "Error saving the image", http.StatusInternalServerError)
			return
		}
		defer outFile.Close()

		_, err = outFile.Write(data)
		if err != nil {
			http.Error(w, "Error writing to file", http.StatusInternalServerError)
			return
		}

		// Get the client IP address
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		} else {
			ips := strings.Split(ip, ", ")
			ip = ips[0]
		}

		// Log the client IP address, file size, and file name
		log.Printf("Client IP: %s\n", ip)
		log.Printf("File size: %d bytes\n", len(data))
		log.Printf("File name: %s.jpg\n", filename)

		w.WriteHeader(http.StatusOK)
	}
}

func drawText(img *image.RGBA, text string, x, y int, col color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
		Dot:  fixed.Point26_6{fixed.Int26_6(x * 64), fixed.Int26_6(y * 64)},
	}
	d.DrawString(text)
}

func main() {
	dirPtr := flag.String("dir", "./", "Directory to save the images")
	maxSizePtr := flag.Int64("maxSize", 1e6, "Max image size in bytes")
	bearerTokenPtr := flag.String("token", "", "Bearer token for authentication")
	portPtr := flag.String("port", "8080", "Port to listen on")
	ipPtr := flag.String("ip", "0.0.0.0", "IP address to listen on")

	flag.Parse()

	if *bearerTokenPtr == "" {
		log.Fatalf("Bearer token must be provided.")
	}

	http.HandleFunc("/save", saveHandler(*dirPtr, *maxSizePtr, *bearerTokenPtr))
	http.HandleFunc("/scaled", scaledHandler(*dirPtr))

	bindAddress := fmt.Sprintf("%s:%s", *ipPtr, *portPtr)
	log.Printf("Listening on %s, saving files to: %s with max size: %d bytes", bindAddress, *dirPtr, *maxSizePtr)
	err := http.ListenAndServe(bindAddress, nil)
	if err != nil {
		log.Fatalf("Could not start server: %s", err.Error())
	}
}
