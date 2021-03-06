package azblob_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	chk "gopkg.in/check.v1"

	"math/rand"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/azure-storage-blob-go/azblob"
)

// For testing docs, see: https://labix.org/gocheck
// To test a specific test: go test -check.f MyTestSuite

// Hookup to the testing framework
func Test(t *testing.T) { chk.TestingT(t) }

type aztestsSuite struct{}

var _ = chk.Suite(&aztestsSuite{})

func (s *aztestsSuite) TestRetryPolicyRetryReadsFromSecondaryHostField(c *chk.C) {
	_, found := reflect.TypeOf(azblob.RetryOptions{}).FieldByName("RetryReadsFromSecondaryHost")
	if !found {
		// Make sure the RetryOption was not erroneously overwritten
		c.Fatal("RetryOption's RetryReadsFromSecondaryHost field must exist in the Blob SDK - uncomment it and make sure the field is returned from the retryReadsFromSecondaryHost() method too!")
	}
}

const (
	containerPrefix             = "go"
	blobPrefix                  = "gotestblob"
	blockBlobDefaultData        = "GoBlockBlobData"
	validationErrorSubstring    = "validation failed"
	invalidHeaderErrorSubstring = "invalid header field" // error thrown by the http client
)

var ctx = context.Background()
var basicHeaders = azblob.BlobHTTPHeaders{
	ContentType:        "my_type",
	ContentDisposition: "my_disposition",
	CacheControl:       "control",
	ContentMD5:         nil,
	ContentLanguage:    "my_language",
	ContentEncoding:    "my_encoding",
}

var basicMetadata = azblob.Metadata{"foo": "bar"}

type testPipeline struct{}

const testPipelineMessage string = "Test factory invoked"

func (tm testPipeline) Do(ctx context.Context, methodFactory pipeline.Factory, request pipeline.Request) (pipeline.Response, error) {
	return nil, errors.New(testPipelineMessage)
}

// This function generates an entity name by concatenating the passed prefix,
// the name of the test requesting the entity name, and the minute, second, and nanoseconds of the call.
// This should make it easy to associate the entities with their test, uniquely identify
// them, and determine the order in which they were created.
// Note that this imposes a restriction on the length of test names
func generateName(prefix string) string {
	// These next lines up through the for loop are obtaining and walking up the stack
	// trace to extrat the test name, which is stored in name
	pc := make([]uintptr, 10)
	runtime.Callers(0, pc)
	f := runtime.FuncForPC(pc[0])
	name := f.Name()
	for i := 0; !strings.Contains(name, "Suite"); i++ { // The tests are all scoped to the suite, so this ensures getting the actual test name
		f = runtime.FuncForPC(pc[i])
		name = f.Name()
	}
	funcNameStart := strings.Index(name, "Test")
	name = name[funcNameStart+len("Test"):] // Just get the name of the test and not any of the garbage at the beginning
	name = strings.ToLower(name)            // Ensure it is a valid resource name
	currentTime := time.Now()
	name = fmt.Sprintf("%s%s%d%d%d", prefix, strings.ToLower(name), currentTime.Minute(), currentTime.Second(), currentTime.Nanosecond())
	return name
}

func generateContainerName() string {
	return generateName(containerPrefix)
}

func generateBlobName() string {
	return generateName(blobPrefix)
}

func getContainerURL(c *chk.C, bsu azblob.ServiceURL) (container azblob.ContainerURL, name string) {
	name = generateContainerName()
	container = bsu.NewContainerURL(name)

	return container, name
}

func getBlockBlobURL(c *chk.C, container azblob.ContainerURL) (blob azblob.BlockBlobURL, name string) {
	name = generateBlobName()
	blob = container.NewBlockBlobURL(name)

	return blob, name
}

func getAppendBlobURL(c *chk.C, container azblob.ContainerURL) (blob azblob.AppendBlobURL, name string) {
	name = generateBlobName()
	blob = container.NewAppendBlobURL(name)

	return blob, name
}

func getPageBlobURL(c *chk.C, container azblob.ContainerURL) (blob azblob.PageBlobURL, name string) {
	name = generateBlobName()
	blob = container.NewPageBlobURL(name)

	return
}

func getReaderToRandomBytes(n int) *bytes.Reader {
	r, _ := getRandomDataAndReader(n)
	return r
}

func getRandomDataAndReader(n int) (*bytes.Reader, []byte) {
	data := make([]byte, n, n)
	rand.Read(data)
	return bytes.NewReader(data), data
}

func createNewContainer(c *chk.C, bsu azblob.ServiceURL) (container azblob.ContainerURL, name string) {
	container, name = getContainerURL(c, bsu)

	cResp, err := container.Create(ctx, nil, azblob.PublicAccessNone)
	c.Assert(err, chk.IsNil)
	c.Assert(cResp.StatusCode(), chk.Equals, 201)
	return container, name
}

func createNewContainerWithSuffix(c *chk.C, bsu azblob.ServiceURL, suffix string) (container azblob.ContainerURL, name string) {
	// The goal of adding the suffix is to be able to predetermine what order the containers will be in when listed.
	// We still need the container prefix to come first, though, to ensure only containers as a part of this test
	// are listed at all.
	name = generateName(containerPrefix + suffix)
	container = bsu.NewContainerURL(name)

	cResp, err := container.Create(ctx, nil, azblob.PublicAccessNone)
	c.Assert(err, chk.IsNil)
	c.Assert(cResp.StatusCode(), chk.Equals, 201)
	return container, name
}

func createNewBlockBlob(c *chk.C, container azblob.ContainerURL) (blob azblob.BlockBlobURL, name string) {
	blob, name = getBlockBlobURL(c, container)

	cResp, err := blob.Upload(ctx, strings.NewReader(blockBlobDefaultData), azblob.BlobHTTPHeaders{},
		nil, azblob.BlobAccessConditions{})

	c.Assert(err, chk.IsNil)
	c.Assert(cResp.StatusCode(), chk.Equals, 201)

	return
}

func createNewAppendBlob(c *chk.C, container azblob.ContainerURL) (blob azblob.AppendBlobURL, name string) {
	blob, name = getAppendBlobURL(c, container)

	resp, err := blob.Create(ctx, azblob.BlobHTTPHeaders{}, nil, azblob.BlobAccessConditions{})

	c.Assert(err, chk.IsNil)
	c.Assert(resp.StatusCode(), chk.Equals, 201)
	return
}

func createNewPageBlob(c *chk.C, container azblob.ContainerURL) (blob azblob.PageBlobURL, name string) {
	blob, name = getPageBlobURL(c, container)

	resp, err := blob.Create(ctx, azblob.PageBlobPageBytes*10, 0, azblob.BlobHTTPHeaders{}, nil, azblob.BlobAccessConditions{})
	c.Assert(err, chk.IsNil)
	c.Assert(resp.StatusCode(), chk.Equals, 201)
	return
}

func createNewPageBlobWithSize(c *chk.C, container azblob.ContainerURL, sizeInBytes int64) (blob azblob.PageBlobURL, name string) {
	blob, name = getPageBlobURL(c, container)

	resp, err := blob.Create(ctx, sizeInBytes, 0, azblob.BlobHTTPHeaders{}, nil, azblob.BlobAccessConditions{})

	c.Assert(err, chk.IsNil)
	c.Assert(resp.StatusCode(), chk.Equals, 201)
	return
}

func createBlockBlobWithPrefix(c *chk.C, container azblob.ContainerURL, prefix string) (blob azblob.BlockBlobURL, name string) {
	name = prefix + generateName(blobPrefix)
	blob = container.NewBlockBlobURL(name)

	cResp, err := blob.Upload(ctx, strings.NewReader(blockBlobDefaultData), azblob.BlobHTTPHeaders{},
		nil, azblob.BlobAccessConditions{})

	c.Assert(err, chk.IsNil)
	c.Assert(cResp.StatusCode(), chk.Equals, 201)
	return
}

func deleteContainer(c *chk.C, container azblob.ContainerURL) {
	resp, err := container.Delete(ctx, azblob.ContainerAccessConditions{})
	c.Assert(err, chk.IsNil)
	c.Assert(resp.StatusCode(), chk.Equals, 202)
}

func getGenericCredential(accountType string) (*azblob.SharedKeyCredential, error) {
	accountNameEnvVar := accountType + "ACCOUNT_NAME"
	accountKeyEnvVar := accountType + "ACCOUNT_KEY"
	accountName, accountKey := os.Getenv(accountNameEnvVar), os.Getenv(accountKeyEnvVar)
	if accountName == "" || accountKey == "" {
		return nil, errors.New(accountNameEnvVar + " and/or " + accountKeyEnvVar + " environment variables not specified.")
	}
	return azblob.NewSharedKeyCredential(accountName, accountKey)
}

func getGenericBSU(accountType string) (azblob.ServiceURL, error) {
	credential, err := getGenericCredential(accountType)
	if err != nil {
		return azblob.ServiceURL{}, err
	}

	pipeline := azblob.NewPipeline(credential, azblob.PipelineOptions{})
	blobPrimaryURL, _ := url.Parse("https://" + credential.AccountName() + ".blob.core.windows.net/")
	return azblob.NewServiceURL(*blobPrimaryURL, pipeline), nil
}

func getBSU() azblob.ServiceURL {
	bsu, _ := getGenericBSU("")
	return bsu
}

func getAlternateBSU() (azblob.ServiceURL, error) {
	return getGenericBSU("SECONDARY_")
}

func getPremiumBSU() (azblob.ServiceURL, error) {
	return getGenericBSU("PREMIUM_")
}

func getBlobStorageBSU() (azblob.ServiceURL, error) {
	return getGenericBSU("BLOB_STORAGE_")
}

func validateStorageError(c *chk.C, err error, code azblob.ServiceCodeType) {
	serr, _ := err.(azblob.StorageError)
	c.Assert(serr.ServiceCode(), chk.Equals, code)
}

func getRelativeTimeGMT(amount time.Duration) time.Time {
	currentTime := time.Now().In(time.FixedZone("GMT", 0))
	currentTime = currentTime.Add(amount * time.Second)
	return currentTime
}

func generateCurrentTimeWithModerateResolution() time.Time {
	highResolutionTime := time.Now().UTC()
	return time.Date(highResolutionTime.Year(), highResolutionTime.Month(), highResolutionTime.Day(), highResolutionTime.Hour(), highResolutionTime.Minute(),
		highResolutionTime.Second(), 0, highResolutionTime.Location())
}

// Some tests require setting service properties. It can take up to 30 seconds for the new properties to be reflected across all FEs.
// We will enable the necessary property and try to run the test implementation. If it fails with an error that should be due to
// those changes not being reflected yet, we will wait 30 seconds and try the test again. If it fails this time for any reason,
// we fail the test. It is the responsibility of the the testImplFunc to determine which error string indicates the test should be retried.
// There can only be one such string. All errors that cannot be due to this detail should be asserted and not returned as an error string.
func runTestRequiringServiceProperties(c *chk.C, bsu azblob.ServiceURL, code string,
	enableServicePropertyFunc func(*chk.C, azblob.ServiceURL),
	testImplFunc func(*chk.C, azblob.ServiceURL) error,
	disableServicePropertyFunc func(*chk.C, azblob.ServiceURL)) {
	enableServicePropertyFunc(c, bsu)
	defer disableServicePropertyFunc(c, bsu)
	err := testImplFunc(c, bsu)
	// We cannot assume that the error indicative of slow update will necessarily be a StorageError. As in ListBlobs.
	if err != nil && err.Error() == code {
		time.Sleep(time.Second * 30)
		err = testImplFunc(c, bsu)
		c.Assert(err, chk.IsNil)
	}
}

func enableSoftDelete(c *chk.C, bsu azblob.ServiceURL) {
	days := int32(1)
	_, err := bsu.SetProperties(ctx, azblob.StorageServiceProperties{DeleteRetentionPolicy: &azblob.RetentionPolicy{Enabled: true, Days: &days}})
	c.Assert(err, chk.IsNil)
}

func disableSoftDelete(c *chk.C, bsu azblob.ServiceURL) {
	_, err := bsu.SetProperties(ctx, azblob.StorageServiceProperties{DeleteRetentionPolicy: &azblob.RetentionPolicy{Enabled: false}})
	c.Assert(err, chk.IsNil)
}

func validateUpload(c *chk.C, blobURL azblob.BlockBlobURL) {
	resp, err := blobURL.Download(ctx, 0, 0, azblob.BlobAccessConditions{}, false)
	c.Assert(err, chk.IsNil)
	data, _ := ioutil.ReadAll(resp.Response().Body)
	c.Assert(data, chk.HasLen, 0)
}

func getRawBlobURLWithSAS(c *chk.C, containerName string, blobName string) url.URL {
	credential, err := getGenericCredential("")
	c.Assert(err, chk.IsNil)
	containerURLWithSAS := getContainerURLWithSAS(c, *credential, containerName)
	blobURLWithSAS := containerURLWithSAS.NewBlockBlobURL(blobName)
	return blobURLWithSAS.URL()
}

func getContainerURLWithSAS(c *chk.C, credential azblob.SharedKeyCredential, containerName string) azblob.ContainerURL {
	sasQueryParams, err := azblob.BlobSASSignatureValues{
		Protocol:      azblob.SASProtocolHTTPS,
		ExpiryTime:    time.Now().UTC().Add(48 * time.Hour),
		ContainerName: containerName,
		Permissions:   azblob.ContainerSASPermissions{Read: true, Add: true, Write: true, Create: true, Delete: true, List: true}.String(),
		Version:       "2018-03-28",
	}.NewSASQueryParameters(&credential)
	c.Assert(err, chk.IsNil)

	// construct the url from scratch
	qp := sasQueryParams.Encode()
	rawURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s?%s",
		credential.AccountName(), containerName, qp)

	// convert the raw url and validate it was parsed successfully
	fullURL, err := url.Parse(rawURL)
	c.Assert(err, chk.IsNil)

	return azblob.NewContainerURL(*fullURL, azblob.NewPipeline(azblob.NewAnonymousCredential(), azblob.PipelineOptions{}))
}
