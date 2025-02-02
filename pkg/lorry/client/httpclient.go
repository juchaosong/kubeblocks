/*
Copyright (C) 2022-2023 ApeCloud Co., Ltd

This file is part of KubeBlocks project

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	intctrlutil "github.com/apecloud/kubeblocks/pkg/controllerutil"
	. "github.com/apecloud/kubeblocks/pkg/lorry/util"
)

const (
	urlTemplate = "http://%s:%d/v1.0/"
)

type HTTPClient struct {
	Client           *http.Client
	URL              string
	cache            map[string]*OperationResult
	CacheTTL         time.Duration
	ReconcileTimeout time.Duration
	RequestTimeout   time.Duration
}

var _ Client = &HTTPClient{}

type OperationResult struct {
	response *http.Response
	err      error
	respTime time.Time
}

func NewHTTPClientWithPod(pod *corev1.Pod) (*HTTPClient, error) {
	ip := pod.Status.PodIP
	if ip == "" {
		return nil, fmt.Errorf("pod %v has no ip", pod.Name)
	}

	port, err := intctrlutil.GetLorryHTTPPort(pod)
	if err != nil {
		// not lorry in the pod, just return nil without error
		return nil, nil
	}

	// don't use default http-client
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}
	netTransport := &http.Transport{
		Dial:                dialer.Dial,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	client := &http.Client{
		Timeout:   time.Second * 30,
		Transport: netTransport,
	}

	operationClient := &HTTPClient{
		Client:           client,
		URL:              fmt.Sprintf(urlTemplate, ip, port),
		CacheTTL:         60 * time.Second,
		RequestTimeout:   30 * time.Second,
		ReconcileTimeout: 500 * time.Millisecond,
		cache:            make(map[string]*OperationResult),
	}
	return operationClient, nil
}

func NewHTTPClientWithURL(url string) (*HTTPClient, error) {
	if url == "" {
		return nil, fmt.Errorf("no url")
	}

	// don't use default http-client
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}
	netTransport := &http.Transport{
		Dial:                dialer.Dial,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	client := &http.Client{
		Timeout:   time.Second * 30,
		Transport: netTransport,
	}

	operationClient := &HTTPClient{
		Client:           client,
		URL:              url,
		CacheTTL:         60 * time.Second,
		RequestTimeout:   30 * time.Second,
		ReconcileTimeout: 500 * time.Millisecond,
		cache:            make(map[string]*OperationResult),
	}
	return operationClient, nil
}

func (cli *HTTPClient) GetRole(ctx context.Context) (string, error) {
	resp, err := cli.Request(ctx, string(GetRoleOperation), http.MethodGet, nil)
	if err != nil {
		return "", err
	}

	role, ok := resp["role"]
	if !ok {
		return "", nil
	}

	return role.(string), nil
}

func (cli *HTTPClient) CreateUser(ctx context.Context, userName, password string) error {
	parameters := map[string]any{
		"userName": userName,
		"password": password,
	}
	req := map[string]any{"parameters": parameters}
	_, err := cli.Request(ctx, string(CreateUserOp), http.MethodPost, req)
	return err
}

func (cli *HTTPClient) DeleteUser(ctx context.Context, userName string) error {
	parameters := map[string]any{
		"userName": userName,
	}
	req := map[string]any{"parameters": parameters}
	_, err := cli.Request(ctx, string(DeleteUserOp), http.MethodPost, req)
	return err
}

func (cli *HTTPClient) DescribeUser(ctx context.Context, userName string) (map[string]any, error) {
	parameters := map[string]any{
		"userName": userName,
	}
	req := map[string]any{"parameters": parameters}
	resp, err := cli.Request(ctx, string(DescribeUserOp), http.MethodGet, req)
	if err != nil {
		return nil, err
	}
	user, ok := resp["user"]
	if !ok {
		return nil, nil
	}

	return user.(map[string]any), nil
}

func (cli *HTTPClient) GrantUserRole(ctx context.Context, userName, roleName string) error {
	parameters := map[string]any{
		"userName": userName,
		"roleName": roleName,
	}
	req := map[string]any{"parameters": parameters}
	_, err := cli.Request(ctx, string(GrantUserRoleOp), http.MethodPost, req)
	return err
}

func (cli *HTTPClient) RevokeUserRole(ctx context.Context, userName, roleName string) error {
	parameters := map[string]any{
		"userName": userName,
		"roleName": roleName,
	}
	req := map[string]any{"parameters": parameters}
	_, err := cli.Request(ctx, string(RevokeUserRoleOp), http.MethodPost, req)
	return err
}

// ListUsers lists all normal users created
func (cli *HTTPClient) ListUsers(ctx context.Context) ([]map[string]any, error) {
	resp, err := cli.Request(ctx, string(ListUsersOp), http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	users, ok := resp["users"]
	if !ok {
		return nil, nil
	}
	return convertToArrayOfMap(users)
}

// ListSystemAccounts lists all system accounts created
func (cli *HTTPClient) ListSystemAccounts(ctx context.Context) ([]map[string]any, error) {
	resp, err := cli.Request(ctx, string(ListSystemAccountsOp), http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	systemAccounts, ok := resp["systemAccounts"]
	if !ok {
		return nil, nil
	}
	return convertToArrayOfMap(systemAccounts)
}

// JoinMember sends a join member operation request to Lorry, located on the target pod that is about to join.
func (cli *HTTPClient) JoinMember(ctx context.Context) error {
	_, err := cli.Request(ctx, string(JoinMemberOperation), http.MethodPost, nil)
	return err
}

// LeaveMember sends a Leave member operation request to Lorry, located on the target pod that is about to leave.
func (cli *HTTPClient) LeaveMember(ctx context.Context) error {
	_, err := cli.Request(ctx, string(LeaveMemberOperation), http.MethodPost, nil)
	return err
}

func (cli *HTTPClient) Request(ctx context.Context, operation, method string, req map[string]any) (map[string]any, error) {
	ctxWithReconcileTimeout, cancel := context.WithTimeout(ctx, cli.ReconcileTimeout)
	defer cancel()

	// Request sql channel via http request
	url := fmt.Sprintf("%s%s", cli.URL, strings.ToLower(operation))

	var reader io.Reader = nil
	if req != nil {
		body, err := json.Marshal(req)
		if err != nil {
			return nil, errors.Wrap(err, "request encode failed")
		}
		reader = bytes.NewReader(body)
	}

	resp, err := cli.InvokeComponentInRoutine(ctxWithReconcileTimeout, url, method, reader)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusUnavailableForLegalReasons:
		return parseBody(resp.Body)
	case http.StatusNoContent:
		return nil, nil
	case http.StatusNotImplemented, http.StatusInternalServerError:
		fallthrough
	default:
		msg, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf(string(msg))
	}
}

func (cli *HTTPClient) InvokeComponentInRoutine(ctxWithReconcileTimeout context.Context, url, method string, body io.Reader) (*http.Response, error) {
	ch := make(chan *OperationResult, 1)
	go cli.InvokeComponent(ctxWithReconcileTimeout, url, method, body, ch)
	var resp *http.Response
	var err error
	select {
	case <-ctxWithReconcileTimeout.Done():
		err = fmt.Errorf("invoke error : %v", ctxWithReconcileTimeout.Err())
	case result := <-ch:
		resp = result.response
		err = result.err
	}
	return resp, err
}

func (cli *HTTPClient) InvokeComponent(ctxWithReconcileTimeout context.Context, url, method string, body io.Reader, ch chan *OperationResult) {
	ctxWithRequestTimeout, cancel := context.WithTimeout(context.Background(), cli.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctxWithRequestTimeout, method, url, body)
	if err != nil {
		operationRes := &OperationResult{
			response: nil,
			err:      err,
			respTime: time.Now(),
		}
		ch <- operationRes
		return
	}

	mapKey := GetMapKeyFromRequest(req)
	operationRes, ok := cli.cache[mapKey]
	if ok {
		delete(cli.cache, mapKey)
		if time.Since(operationRes.respTime) <= cli.CacheTTL {
			ch <- operationRes
			return
		}
	}

	resp, err := cli.Client.Do(req)
	operationRes = &OperationResult{
		response: resp,
		err:      err,
		respTime: time.Now(),
	}
	select {
	case <-ctxWithReconcileTimeout.Done():
		cli.cache[mapKey] = operationRes
	default:
		ch <- operationRes
	}
}

func GetMapKeyFromRequest(req *http.Request) string {
	var buf bytes.Buffer
	buf.WriteString(req.URL.String())

	if req.Body != nil {
		all, err := io.ReadAll(req.Body)
		if err != nil {
			return ""
		}
		req.Body = io.NopCloser(bytes.NewReader(all))
		buf.Write(all)
	}
	keys := make([]string, 0, len(req.Header))
	for k := range req.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		buf.WriteString(fmt.Sprintf("%s:%s", k, req.Header[k]))
	}

	return buf.String()
}

func parseBody(body io.Reader) (map[string]any, error) {
	result := map[string]any{}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, errors.Wrap(err, "read response body failed")
	}
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, errors.Wrap(err, "decode body failed")
	}

	return result, nil
}

func convertToArrayOfMap(value any) ([]map[string]any, error) {
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("resp errors: %v", value)
	}

	result := make([]map[string]any, 0, len(array))
	for _, v := range array {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("resp errors: %v", value)
		}
		result = append(result, m)
	}
	return result, nil
}
