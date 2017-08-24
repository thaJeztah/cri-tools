/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Sirupsen/logrus"
	units "github.com/docker/go-units"
	"github.com/urfave/cli"
	"golang.org/x/net/context"
	pb "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
)

const (
	// truncatedImageIDLen is the truncated length of imageID
	truncatedImageIDLen = 13
)

var pullImageCommand = cli.Command{
	Name:  "pull",
	Usage: "Pull an image from a registry",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "creds",
			Value: "",
			Usage: "Use `USERNAME[:PASSWORD]` for accessing the registry",
		},
	},
	ArgsUsage: "NAME[:TAG|@DIGEST]",
	Action: func(context *cli.Context) error {
		imageName := context.Args().First()
		if imageName == "" {
			return cli.ShowSubcommandHelp(context)
		}

		if err := getImageClient(context); err != nil {
			return err
		}

		var auth *pb.AuthConfig
		if context.IsSet("creds") {
			var err error
			auth, err = getAuth(context.String("creds"))
			if err != nil {
				return err
			}
		}

		r, err := PullImage(imageClient, imageName, auth)
		logrus.Debugf("PullImageResponse: %v", r)
		if err != nil {
			return fmt.Errorf("pulling image failed: %v", err)
		}
		fmt.Printf("Image is update to date for %s\n", r.ImageRef)
		return nil
	},
}

var listImageCommand = cli.Command{
	Name:      "images",
	Usage:     "List images",
	ArgsUsage: "[REPOSITORY[:TAG]]",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "verbose, v",
			Usage: "Show verbose info for images",
		},
		cli.BoolFlag{
			Name:  "quiet, q",
			Usage: "Only show image IDs",
		},
		cli.StringFlag{
			Name:  "output, o",
			Usage: "Output format, One of: json|yaml|table",
		},
	},
	Action: func(context *cli.Context) error {
		if err := getImageClient(context); err != nil {
			return err
		}

		r, err := ListImages(imageClient, context.Args().First())
		logrus.Debugf("ListImagesResponse: %v", r)
		if err != nil {
			return fmt.Errorf("listing images failed: %v", err)
		}

		switch context.String("output") {
		case "json":
			return outputJson(r.Images)
		case "yaml":
			return outputYaml(r.Images)
		}

		// output in table format by default.
		w := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
		verbose := context.Bool("verbose")
		printHeader := true
		for _, image := range r.Images {
			if context.Bool("quiet") {
				fmt.Printf("%s\n", image.Id)
				continue
			}
			if !verbose {
				if printHeader {
					printHeader = false
					fmt.Fprintln(w, "IMAGE\tTAG\tIMAGE ID\tSIZE")
				}
				repoTags := "<none>"
				if image.RepoTags != nil {
					repoTags = image.RepoTags[0]
				}
				repoTagsPair := strings.Split(repoTags, ":")
				size := units.HumanSizeWithPrecision(float64(image.GetSize_()), 3)
				trunctedImage := strings.TrimPrefix(image.Id, "sha256:")[:truncatedImageIDLen]
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", repoTagsPair[0], repoTagsPair[1], trunctedImage, size)
				continue
			}
			fmt.Printf("ID: %s\n", image.Id)
			for _, tag := range image.RepoTags {
				fmt.Printf("RepoTags: %s\n", tag)
			}
			for _, digest := range image.RepoDigests {
				fmt.Printf("RepoDigests: %s\n", digest)
			}
			if image.Size_ != 0 {
				fmt.Printf("Size: %d\n", image.Size_)
			}
			if image.Uid != nil {
				fmt.Printf("Uid: %v\n", image.Uid)
			}
			if image.Username != "" {
				fmt.Printf("Username: %v\n\n", image.Username)
			}
		}
		w.Flush()
		return nil
	},
}

var imageStatusCommand = cli.Command{
	Name:      "inspecti",
	Usage:     "Return the status of an image",
	ArgsUsage: "IMAGEID",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "output, o",
			Usage: "Output format, One of: json|yaml|table",
		},
	},
	Action: func(context *cli.Context) error {
		id := context.Args().First()
		if id == "" {
			return cli.ShowSubcommandHelp(context)
		}

		if err := getImageClient(context); err != nil {
			return err
		}

		r, err := ImageStatus(imageClient, id)
		logrus.Debugf("ImageStatus: %v", r)
		if err != nil {
			return fmt.Errorf("image status request failed: %v", err)
		}
		image := r.Image
		if image == nil {
			return fmt.Errorf("no such image present")
		}

		switch context.String("output") {
		case "json":
			return outputJson(r.Image)

		case "yaml":
			return outputYaml(r.Image)
		}

		// output in table format by default.
		fmt.Printf("ID: %s\n", image.Id)
		for _, tag := range image.RepoTags {
			fmt.Printf("Tag: %s\n", tag)
		}
		for _, digest := range image.RepoDigests {
			fmt.Printf("Digest: %s\n", digest)
		}
		size := units.HumanSizeWithPrecision(float64(image.GetSize_()), 3)
		fmt.Printf("Size: %s\n", size)

		return nil
	},
}

var removeImageCommand = cli.Command{
	Name:      "rmi",
	Usage:     "Remove an image",
	ArgsUsage: "IMAGEID",
	Action: func(context *cli.Context) error {
		id := context.Args().First()
		if id == "" {
			return cli.ShowSubcommandHelp(context)
		}

		if err := getImageClient(context); err != nil {
			return err
		}

		status, err := ImageStatus(imageClient, id)
		logrus.Debugf("Get image status: %v", status)
		if err != nil {
			return fmt.Errorf("image status request failed: %v", err)
		}
		if status.Image == nil {
			return fmt.Errorf("no such image %s", id)
		}

		r, err := RemoveImage(imageClient, id)
		logrus.Debugf("Get remove image response: %v", r)
		if err != nil {
			return fmt.Errorf("error of removing image %q: %v", id, err)
		}
		for _, repoTag := range status.Image.RepoTags {
			fmt.Printf("Deleted: %s\n", repoTag)
		}
		return nil
	},
}

func parseCreds(creds string) (string, string, error) {
	if creds == "" {
		return "", "", errors.New("credentials can't be empty")
	}
	up := strings.SplitN(creds, ":", 2)
	if len(up) == 1 {
		return up[0], "", nil
	}
	if up[0] == "" {
		return "", "", errors.New("username can't be empty")
	}
	return up[0], up[1], nil
}

func getAuth(creds string) (*pb.AuthConfig, error) {
	username, password, err := parseCreds(creds)
	if err != nil {
		return nil, err
	}
	return &pb.AuthConfig{
		Username: username,
		Password: password,
	}, nil
}

// PullImage sends a PullImageRequest to the server, and parses
// the returned PullImageResponse.
func PullImage(client pb.ImageServiceClient, image string, auth *pb.AuthConfig) (*pb.PullImageResponse, error) {
	request := &pb.PullImageRequest{
		Image: &pb.ImageSpec{
			Image: image,
		},
	}
	if auth != nil {
		request.Auth = auth
	}
	logrus.Debugf("PullImageRequest: %v", request)
	return client.PullImage(context.Background(), request)
}

// ListImages sends a ListImagesRequest to the server, and parses
// the returned ListImagesResponse.
func ListImages(client pb.ImageServiceClient, image string) (*pb.ListImagesResponse, error) {
	request := &pb.ListImagesRequest{Filter: &pb.ImageFilter{Image: &pb.ImageSpec{Image: image}}}
	logrus.Debugf("ListImagesRequest: %v", request)
	return client.ListImages(context.Background(), &pb.ListImagesRequest{Filter: &pb.ImageFilter{Image: &pb.ImageSpec{Image: image}}})
}

// ImageStatus sends an ImageStatusRequest to the server, and parses
// the returned ImageStatusResponse.
func ImageStatus(client pb.ImageServiceClient, image string) (*pb.ImageStatusResponse, error) {
	request := &pb.ImageStatusRequest{Image: &pb.ImageSpec{Image: image}}
	logrus.Debugf("ImageStatusRequest: %v", request)
	return client.ImageStatus(context.Background(), &pb.ImageStatusRequest{Image: &pb.ImageSpec{Image: image}})
}

// RemoveImage sends a RemoveImageRequest to the server, and parses
// the returned RemoveImageResponse.
func RemoveImage(client pb.ImageServiceClient, image string) (*pb.RemoveImageResponse, error) {
	if image == "" {
		return nil, fmt.Errorf("ImageID cannot be empty")
	}
	request := &pb.RemoveImageRequest{Image: &pb.ImageSpec{Image: image}}
	logrus.Debugf("RemoveImageRequest: %v", request)
	return client.RemoveImage(context.Background(), &pb.RemoveImageRequest{Image: &pb.ImageSpec{Image: image}})
}
