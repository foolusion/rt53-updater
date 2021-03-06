/* Copyright 2016 Andrew O'Neill

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
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"k8s.io/client-go/1.5/kubernetes"
	"k8s.io/client-go/1.5/pkg/api"
	"k8s.io/client-go/1.5/pkg/api/v1"
	"k8s.io/client-go/1.5/pkg/labels"
	"k8s.io/client-go/1.5/pkg/selection"
	"k8s.io/client-go/1.5/pkg/util/sets"
	"k8s.io/client-go/1.5/pkg/watch"
	"k8s.io/client-go/1.5/rest"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/foolusion/certs"
	"github.com/pkg/errors"
)

const (
	envNamespace     = "NAMESPACE"
	envAWSRegion     = "AWS_REGION"
	envAnnDNSName    = "DNS_NAME_ANNOTATION"
	envAnnHostedZone = "HOSTED_ZONE_ANNOTATION"

	annotationDNSName    = "foolusion-aws-route53-dns-name"
	annotationHostedZone = "foolusion-aws-route53-hosted-zone"
)

type watcherErr struct{}

func (w watcherErr) Error() string {
	return "watch chan closed"
}

type updaterConfig struct {
	namespace            string
	region               string
	annotationDNSName    string
	annotationHostedZone string
}

var cfg = updaterConfig{
	namespace:            "",
	region:               "us-west-2",
	annotationDNSName:    annotationDNSName,
	annotationHostedZone: annotationHostedZone,
}

var sess *session.Session

func main() {
	log.Println("Starting rt53-updater operator...")

	if region := os.Getenv(envAWSRegion); region != "" {
		cfg.region = region
	}
	log.Printf("using region %q", cfg.region)

	if namespace := os.Getenv(envNamespace); namespace != "" {
		cfg.namespace = namespace
	}
	log.Printf("using namespace %q", cfg.namespace)

	if dnsName := os.Getenv(envAnnDNSName); dnsName != "" {
		cfg.annotationDNSName = dnsName
	}
	log.Printf("using dns name annotation %q", cfg.annotationDNSName)

	if hostedZone := os.Getenv(envAnnHostedZone); hostedZone != "" {
		cfg.annotationHostedZone = hostedZone
	}
	log.Printf("using hosted zone annotation %q", cfg.annotationHostedZone)

	sess = session.Must(session.NewSession(&aws.Config{
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: certs.Pool,
				},
			},
		},
		Region: aws.String(cfg.region),
	}))

	errCh := make(chan error, 1)
	cancel, err := watchService(errCh)
	if err != nil {
		close(errCh)
		log.Fatal(err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case err := <-errCh:
			switch e := errors.Cause(err).(type) {
			case *watcherErr:
				cancel, err = watchService(errCh)
				if err != nil {
					shutdown(errCh, sigCh, cancel, 1)
				}
			case *fatalErr:
				log.Println(e)
				shutdown(errCh, sigCh, cancel, 1)
			default:
				log.Println(e)
			}
		case <-sigCh:
			log.Println("Shutdown signal recieved, exiting...")
			shutdown(errCh, sigCh, cancel, 0)
		}
	}
}

func shutdown(errCh chan error, sigCh chan os.Signal, cancel context.CancelFunc, status int) {
	close(sigCh)
	cancel()
	close(errCh)
	os.Exit(status)
}

type fatalErr struct {
	err error
}

func (f *fatalErr) Error() string {
	return fmt.Sprintf("fatal error: %s", f.err.Error())
}

func createWatcher() (watch.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.Wrap(err, "could not get in cluster config")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "could not create a new in cluster client")
	}

	ls := labels.NewSelector()
	req, err := labels.NewRequirement("route53", selection.Equals, sets.NewString("loadBalancer"))
	if err != nil {
		return nil, errors.Wrap(err, "could not create label requirements")
	}
	ls = ls.Add(*req)
	watcher, err := clientset.Core().Services(cfg.namespace).Watch(api.ListOptions{
		LabelSelector: ls,
	})
	if err != nil {
		return nil, errors.Wrap(err, "could not create service watcher")
	}
	return watcher, nil
}

func watchService(errCh chan<- error) (context.CancelFunc, error) {
	watcher, err := createWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "could not create a watcher")
	}

	ctx, cancel := context.WithCancel(context.Background())

	go serviceWatcher(ctx, watcher, errCh)
	return cancel, nil
}

func serviceWatcher(ctx context.Context, w watch.Interface, errCh chan<- error) {
	go func() {
		<-ctx.Done()
		w.Stop()
	}()

	for ev := range w.ResultChan() {
		err := handleEvent(ev)
		if err != nil {
			errCh <- errors.Wrap(err, "unable to handle event")
		}
	}
	errCh <- watcherErr{}
}

func handleEvent(ev watch.Event) error {
	switch ev.Type {
	case watch.Added, watch.Modified:
		s := ev.Object.(*v1.Service)
		rt, err := updateRoute53(s)
		if err != nil {
			return errors.Wrap(err, "could not get route53 details")
		}
		err = setRoute53(rt)
		if err != nil {
			return errors.Wrap(err, "could not update route53")
		}
		return nil
	default:
		log.Printf("useless event %s", ev)
		return nil
	}
}

func updateRoute53(s *v1.Service) (rt53Config, error) {
	ann := s.GetAnnotations()
	name, ok := ann[cfg.annotationDNSName]
	if !ok {
		return rt53Config{}, errors.Errorf("service %s missing annotation %s", s.GetName(), cfg.annotationDNSName)
	}

	hostedZone, ok := ann[cfg.annotationHostedZone]
	if !ok {
		return rt53Config{}, errors.Errorf("service %s missing annotation %s", s.GetName(), cfg.annotationHostedZone)
	}

	rt53Route := route{
		dnsName:      name,
		hostedZoneID: hostedZone,
	}

	if len(s.Status.LoadBalancer.Ingress) != 1 {
		return rt53Config{}, errors.Errorf("LoadBalancer.Ingress != 1 got %v", len(s.Status.LoadBalancer.Ingress))
	}
	loadBalancerHostname := s.Status.LoadBalancer.Ingress[0].Hostname
	if loadBalancerHostname == "" {
		return rt53Config{}, errors.Errorf("service %s missing LoadBalancer.Ingress", s.GetName())
	}

	loadBalancerName := strings.Split(loadBalancerHostname, "-")[1]

	loadBalancerHostedZoneID, err := getLoadBalancerHostedZone(loadBalancerName)
	if err != nil {
		return rt53Config{}, errors.WithMessage(err, "could not get hosted zone ID from load balancer")
	}

	loadBalancerRoute := route{
		dnsName:      loadBalancerHostname,
		hostedZoneID: loadBalancerHostedZoneID,
	}

	return rt53Config{
		rt53:         rt53Route,
		loadBalancer: loadBalancerRoute,
	}, nil
}

type route struct {
	dnsName      string
	hostedZoneID string
}

type rt53Config struct {
	rt53         route
	loadBalancer route
}

func getLoadBalancerHostedZone(name string) (string, error) {
	svc := elb.New(sess)

	params := &elb.DescribeLoadBalancersInput{
		LoadBalancerNames: []*string{
			aws.String(name),
		},
	}
	resp, err := svc.DescribeLoadBalancers(params)
	if err != nil {
		return "", errors.WithMessage(err, "could not describe load balancers")
	}

	if len(resp.LoadBalancerDescriptions) != 1 {
		return "", errors.Wrapf(err, "length of LoadBalancerDescriptions != 1 got %v", len(resp.LoadBalancerDescriptions))
	}

	hostedZoneID := resp.LoadBalancerDescriptions[0].CanonicalHostedZoneNameID
	if hostedZoneID == nil {
		return "", errors.WithMessage(err, "LoadBalancer hostedZoneID is not set")
	}

	return *hostedZoneID, nil
}

func setRoute53(r rt53Config) error {
	svc := route53.New(sess)

	params := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{
				{
					Action: aws.String(route53.ChangeActionUpsert),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String(r.rt53.dnsName),
						Type: aws.String(route53.RRTypeA),
						AliasTarget: &route53.AliasTarget{
							DNSName:              aws.String(r.loadBalancer.dnsName),
							HostedZoneId:         aws.String(r.loadBalancer.hostedZoneID),
							EvaluateTargetHealth: aws.Bool(false),
						},
					},
				},
			},
		},
		HostedZoneId: aws.String(r.rt53.hostedZoneID),
	}

	resp, err := svc.ChangeResourceRecordSets(params)
	if err != nil {
		return errors.Wrap(err, "could not change record set")
	}

	log.Println(resp)
	return nil
}
