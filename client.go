package bbs

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cloudfoundry-incubator/bbs/models"
	"github.com/cloudfoundry-incubator/cf_http"
	"github.com/gogo/protobuf/proto"
	"github.com/tedsuo/rata"
)

var ErrReadFromClosedSource = errors.New("read from closed source")
var ErrSendToClosedSource = errors.New("send to closed source")
var ErrSourceAlreadyClosed = errors.New("source already closed")
var ErrSlowConsumer = errors.New("slow consumer")

var ErrSubscribedToClosedHub = errors.New("subscribed to closed hub")
var ErrHubAlreadyClosed = errors.New("hub already closed")

const (
	ContentTypeHeader    = "Content-Type"
	XCfRouterErrorHeader = "X-Cf-Routererror"
	ProtoContentType     = "application/x-protobuf"
)

//go:generate counterfeiter -o fake_bbs/fake_client.go . Client

type Client interface {
	Domains() ([]string, error)
	UpsertDomain(domain string, ttl time.Duration) error

	ActualLRPGroups(models.ActualLRPFilter) ([]*models.ActualLRPGroup, error)
	ActualLRPGroupsByProcessGuid(processGuid string) ([]*models.ActualLRPGroup, error)
	ActualLRPGroupByProcessGuidAndIndex(processGuid string, index int) (*models.ActualLRPGroup, error)

	DesiredLRPs(models.DesiredLRPFilter) ([]*models.DesiredLRP, error)
	DesiredLRPByProcessGuid(processGuid string) (*models.DesiredLRP, error)
}

func NewClient(url string) Client {
	return &client{
		httpClient:          cf_http.NewClient(),
		streamingHTTPClient: cf_http.NewStreamingClient(),

		reqGen: rata.NewRequestGenerator(url, Routes),
	}
}

type client struct {
	httpClient          *http.Client
	streamingHTTPClient *http.Client

	reqGen *rata.RequestGenerator
}

func (c *client) Domains() ([]string, error) {
	var domains models.Domains
	err := c.doRequest(DomainsRoute, nil, nil, nil, &domains)
	return domains.GetDomains(), err
}

func (c *client) UpsertDomain(domain string, ttl time.Duration) error {
	req, err := c.createRequest(UpsertDomainRoute, rata.Params{"domain": domain}, nil, nil)
	if err != nil {
		return err
	}

	if ttl != 0 {
		req.Header.Set("Cache-Control", fmt.Sprintf("max-age=%d", int(ttl.Seconds())))
	}

	return c.do(req, nil)
}

func (c *client) ActualLRPGroups(filter models.ActualLRPFilter) ([]*models.ActualLRPGroup, error) {
	var actualLRPGroups models.ActualLRPGroups
	query := url.Values{}
	if filter.Domain != "" {
		query.Set("domain", filter.Domain)
	}
	if filter.CellID != "" {
		query.Set("cell_id", filter.CellID)
	}
	err := c.doRequest(ActualLRPGroupsRoute, nil, query, nil, &actualLRPGroups)
	return actualLRPGroups.GetActualLrpGroups(), err
}

func (c *client) ActualLRPGroupsByProcessGuid(processGuid string) ([]*models.ActualLRPGroup, error) {
	var actualLRPGroups models.ActualLRPGroups
	err := c.doRequest(ActualLRPGroupsByProcessGuidRoute, rata.Params{"process_guid": processGuid}, nil, nil, &actualLRPGroups)
	return actualLRPGroups.GetActualLrpGroups(), err
}

func (c *client) ActualLRPGroupByProcessGuidAndIndex(processGuid string, index int) (*models.ActualLRPGroup, error) {
	var actualLRPGroup models.ActualLRPGroup
	err := c.doRequest(ActualLRPGroupByProcessGuidAndIndexRoute,
		rata.Params{"process_guid": processGuid, "index": strconv.Itoa(index)},
		nil, nil, &actualLRPGroup)
	return &actualLRPGroup, err
}

func (c *client) DesiredLRPs(filter models.DesiredLRPFilter) ([]*models.DesiredLRP, error) {
	var desiredLRPs models.DesiredLRPs
	query := url.Values{}
	if filter.Domain != "" {
		query.Set("domain", filter.Domain)
	}
	err := c.doRequest(DesiredLRPsRoute, nil, query, nil, &desiredLRPs)
	return desiredLRPs.GetDesiredLrps(), err
}

func (c *client) DesiredLRPByProcessGuid(processGuid string) (*models.DesiredLRP, error) {
	var desiredLRP models.DesiredLRP
	err := c.doRequest(DesiredLRPByProcessGuidRoute,
		rata.Params{"process_guid": processGuid},
		nil, nil, &desiredLRP)
	return &desiredLRP, err
}

func (c *client) createRequest(requestName string, params rata.Params, queryParams url.Values, message proto.Message) (*http.Request, error) {
	var messageBody []byte
	var err error
	if message != nil {
		messageBody, err = proto.Marshal(message)
		if err != nil {
			return nil, err
		}
	}

	req, err := c.reqGen.CreateRequest(requestName, params, bytes.NewReader(messageBody))
	if err != nil {
		return nil, err
	}

	req.URL.RawQuery = queryParams.Encode()
	req.ContentLength = int64(len(messageBody))
	req.Header.Set("Content-Type", ProtoContentType)
	return req, nil
}

func (c *client) doRequest(requestName string, params rata.Params, queryParams url.Values, request, message proto.Message) error {
	req, err := c.createRequest(requestName, params, queryParams, request)
	if err != nil {
		return err
	}
	return c.do(req, message)
}

func (c *client) do(req *http.Request, responseObject interface{}) error {
	start := time.Now()
	res, err := c.httpClient.Do(req)
	end := time.Now()
	if err != nil {
		return err
	}
	defer res.Body.Close()
	println("response received", end.Sub(start).String())

	var parsedContentType string
	if contentType, ok := res.Header[ContentTypeHeader]; ok {
		parsedContentType, _, _ = mime.ParseMediaType(contentType[0])
	}

	if routerError, ok := res.Header[XCfRouterErrorHeader]; ok {
		return &Error{Type: proto.String(RouterError), Message: &routerError[0]}
	}

	if parsedContentType == ProtoContentType {
		protoMessage, ok := responseObject.(proto.Message)
		if !ok {
			return &Error{Type: proto.String(InvalidRequest), Message: proto.String("cannot read response body")}
		}
		return handleProtoResponse(res, protoMessage)
	} else {
		return handleNonProtoResponse(res)
	}
}

func handleProtoResponse(res *http.Response, responseObject proto.Message) error {
	start := time.Now()
	buf, err := ioutil.ReadAll(res.Body)
	end := time.Now()
	if err != nil {
		return &Error{Type: proto.String(InvalidResponse), Message: proto.String(err.Error())}
	}
	println("read all", end.Sub(start).String())

	if res.StatusCode > 299 {
		errResponse := &Error{}
		err = proto.Unmarshal(buf, errResponse)
		if err != nil {
			return &Error{Type: proto.String(InvalidProtobufMessage), Message: proto.String(err.Error())}
		}
		return errResponse
	}

	start = time.Now()
	err = proto.Unmarshal(buf, responseObject)
	end = time.Now()
	if err != nil {
		return Error{Type: proto.String(InvalidProtobufMessage), Message: proto.String(err.Error())}
	}
	println("unmarshal", end.Sub(start).String())
	return nil
}

func handleNonProtoResponse(res *http.Response) error {
	if res.StatusCode > 299 {
		return Error{
			Type:    proto.String(InvalidResponse),
			Message: proto.String(fmt.Sprintf("Invalid Response with status code: %d", res.StatusCode)),
		}
	}
	return nil
}
