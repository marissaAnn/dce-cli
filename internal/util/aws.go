package util

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Optum/dce-cli/configs"
	observ "github.com/Optum/dce-cli/internal/observation"
	"github.com/aws/aws-sdk-go/aws"
	awsSession "github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type AWSUtil struct {
	Config      *configs.Root
	Observation *observ.ObservationContainer
	Session     *awsSession.Session
}

func (u *AWSUtil) UploadDirectoryToS3(localPath string, bucket string, prefix string) ([]string, []string) {
	walker := make(fileWalk)
	go func() {
		// Gather the files to upload by walking the path recursively
		if err := filepath.Walk(localPath, walker.Walk); err != nil {
			log.Fatalln("Walk failed:", err)
		}
		close(walker)
	}()

	// For each file found walking, upload it to S3
	lambdas := []string{}
	codebuilds := []string{}
	uploader := s3manager.NewUploader(u.Session)
	for path := range walker {
		rel, err := filepath.Rel(localPath, path)
		if err != nil {
			log.Fatalln("Unable to get relative path:", path, err)
		}
		file, err := os.Open(path)
		if err != nil {
			log.Println("Failed opening file", path, err)
			continue
		}
		defer file.Close()
		result, err := uploader.Upload(&s3manager.UploadInput{
			Bucket: &bucket,
			Key:    aws.String(filepath.Join(prefix, rel)),
			Body:   file,
		})
		if err != nil {
			log.Fatalln("Failed to upload", path, err)
		}
		log.Debugln("Uploaded", path, result.Location)
		log.Infoln(".")

		parent := filepath.Base(filepath.Dir(path))
		if parent == "lambda" {
			lambdas = append(lambdas, filepath.Base(path))
		}
		if parent == "codebuild" {
			codebuilds = append(lambdas, filepath.Base(path))
		}
	}
	return lambdas, codebuilds
}

type fileWalk chan string

func (f fileWalk) Walk(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if !info.IsDir() {
		f <- path
	}
	return nil
}

func (u *AWSUtil) UpdateLambdasFromS3Assets(lambdaNames []string, bucket string, namespace string) {
	client := lambda.New(u.Session)
	log.Infoln("Updating AWS Lambda functions...")

	for _, l := range lambdaNames {

		name := strings.TrimSuffix(l, ".zip")
		log.Debugln("Updating Lambda config for: ", name)

		input := &lambda.UpdateFunctionCodeInput{
			FunctionName: aws.String(name + "-" + namespace),
			Publish:      aws.Bool(true),
			S3Bucket:     aws.String(bucket),
			S3Key:        aws.String("lambda/" + name + ".zip"),
		}

		out, err := json.Marshal(input)
		if err != nil {
			panic(err)
		}

		updateLambdaConfig, _ := client.UpdateFunctionCode(input)

		out, err = json.Marshal(updateLambdaConfig)
		if err != nil {
			panic(err)
		}

		log.Debugln("Updated Lambda config: ", string(out))
	}
	log.Infoln("Finished updating AWS Lambda functions.")
}

func (u *AWSUtil) ConfigureAWSCLICredentials(accessKeyID, secretAccessKey, sessionToken, profile string) {
	_, err := exec.Command("aws", "configure", "--profile", profile, "set", "aws_access_key_id", accessKeyID).CombinedOutput()
	if err != nil {
		log.Fatalln(err)
	}
	_, err = exec.Command("aws", "configure", "--profile", profile, "set", "aws_secret_access_key", secretAccessKey).CombinedOutput()
	if err != nil {
		log.Fatalln(err)
	}
	_, err = exec.Command("aws", "configure", "--profile", profile, "set", "aws_session_token", sessionToken).CombinedOutput()
	if err != nil {
		log.Fatalln(err)
	}
}
