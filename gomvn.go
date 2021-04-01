package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	mvn "github.com/navdeepsekhon/go-mvn-dl/download"
)

var (
	flagMvnServer string
	flagOutputDir string
	flagPrintOnly bool
	flagQuite     bool
)

func log(l string) {
	if !flagQuite {
		fmt.Println(l)
	}
}

func isS3Link(input string) (bool, *url.URL) {
	u, _ := url.Parse(input)
	return strings.ToLower(u.Scheme) == "s3", u
}

func s3Download(inputUrl, user, pwd string) (*http.Response, error) {
	isS3, u := isS3Link(inputUrl)
	if !isS3 {
		return nil, fmt.Errorf("not a s3 url")
	}

	sess := session.Must(session.NewSession())

	// get region
	ec2meta := ec2metadata.New(sess)
	region, err := ec2meta.Region()
	if err != nil {
		region = endpoints.ApSoutheast1RegionID
	}

	// fetch data from s3
	s3client := s3.New(sess, aws.NewConfig().WithRegion(region))
	input := &s3.GetObjectInput{
		Bucket: aws.String(u.Host),
		Key:    aws.String(u.Path[1:]),
	}
	output, err := s3client.GetObject(input)
	if err != nil {
		return nil, err
	}

	resp := &http.Response{
		StatusCode:    200,
		ContentLength: aws.Int64Value(output.ContentLength),
		Body:          output.Body,
	}
	return resp, nil
}

func httpDownload(url, user, pwd string) (*http.Response, error) {
	if user != "" && pwd != "" {
		client := &http.Client{}
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(user, pwd)
		return client.Do(req)
	}

	return http.Get(url)
}

func main() {
	pwd, err := os.Getwd()
	if err != nil {
		os.Exit(127)
	}

	flag.StringVar(&flagMvnServer, "mvn-server", "https://repo1.maven.org/maven2/", "Custom maven server")
	flag.StringVar(&flagOutputDir, "output-dir", pwd, "Directory to save downloaded files")
	flag.BoolVar(&flagPrintOnly, "print-only", false, "Only print paths of downloaded files")
	flag.BoolVar(&flagQuite, "quite", false, "Do not output logs")

	flag.Parse()

	// ). check mvn server input
	if !strings.HasSuffix(flagMvnServer, "/") {
		flagMvnServer = flagMvnServer + "/"
	}

	// ). collect all coordinates into artifacts
	coordinates := flag.Args()
	artifacts := make([]*mvn.Artifact, 0, 3)
	for _, c := range coordinates {
		arti, err := mvn.ParseName(c)
		if err != nil {
			log(fmt.Sprintf("invalid coordinate -> %s", c))
			continue
		}
		arti.RepositoryUrl = flagMvnServer
		artifacts = append(artifacts, &arti)
	}
	if len(artifacts) <= 0 {
		log("there is no coordinate to handle")
		os.Exit(0)
	}

	// ). check if should use s3 downloader
	useS3, _ := isS3Link(flagMvnServer)

	// ). handle each artifact
	for _, a := range artifacts {
		// choose downloader
		if useS3 {
			a.Downloader = s3Download
		} else {
			a.Downloader = httpDownload
		}

		// prepare download url
		finalUrl, err := mvn.ArtifactUrl(*a)
		if err != nil {
			log(fmt.Sprintf("meet error -> %s", err))
			continue
		}
		if flagPrintOnly {
			fmt.Println(finalUrl)
			continue
		}

		// prepare output filepath
		filename := mvn.FileName(*a)
		filepath := path.Join(flagOutputDir, filename)

		out, err := os.Create(filepath)
		if err != nil {
			log(fmt.Sprintf("meet error -> %s", err))
			continue
		}
		defer out.Close()

		// do download
		resp, err := a.Downloader(finalUrl, a.RepoUser, a.RepoPassword)
		if err != nil {
			log(fmt.Sprintf("meet error -> %s", err))
			continue
		}
		defer resp.Body.Close()

		// write to file
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			log(fmt.Sprintf("meet error -> %s", err))
			continue
		}

		log(fmt.Sprintf("finish download -> %s", finalUrl))
	}

	log("done!")
}
