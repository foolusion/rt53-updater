# rt53-updater

An app that updates your aws route53 for kubernetes service type loadBalancer

This program is meant to be run in a kubernetes cluster. It talks to the
kubernetes api and watches all Services with the label route53:loadBalancer.
Whenever a change occurs on one of these services it will update the route53 in
AWS. To determine which route53 to edit it uses the annotations
`foolusion-aws-route53-name` and `foolusion-aws-route53-hostedZone` on the
service. It then queries for the elastic load balancer that matches the
services ingress. It finds the elastic load balancer's HostedZone and then
calls ChangeResourceRecordSets on route53. It creates and AliasTarget Record
Set for the ELB.

## Configuration

There are four environment variables you can use to configure. They are
`NAMESPACE`, `AWS_REGION` `DNS_NAME_ANNOTATION`, and `HOSTED_ZONE_ANNOTATION`.

`NAMESPACE` allows you to filter to only services in the specified namespace.
The default value for `NAMESPACE` is `""`, which is all namespaces.

`AWS_REGION` is required for the query to describe-load-balancers. The default
value for `AWS_REGION` is `us-west-2`. You will need to change this if you are
using a different region.

`DNS_NAME_ANNOTATION` lets you configure the annotation used for deciding the
route53 dns-name. The default value for `DNS_NAME_ANNOTATION` is
`foolusion-aws-route53-dns-name`.

`HOSTED_ZONE_ANNOTATION` lets you configure the annotation used for deciding
the route53 hosted zone. The default value for `HOSTED_ZONE_ANNOTATION` is
`foolusion-aws-route53-hosted-zone`.

## AWS Permissions

In order for aws commands to work the instance profile your cluster workers use
will need permission to `easticloadbalancing:DescribeLoadBalancers` and
`route53.ChangeResourceRecordSets`. You can also try using
[kube2iam](https://github.com/jtblin/kube2iam), which allows your pods to
assume other roles.

## Build

To Build and create deployment artifacts you can run `make`. If you just want
to build the go binary you can run `go build`. If you don't want to build at
all just use the `foolusion/rt53-updater` image from docker hub.

> If you run `make` be sure to change the tagBase to use your image repository.

To deploy take a look at `rt53-updater.yaml` or `kube.yaml` if you used `make`.
Then run `kubectl apply -f your-deployment-file.yaml`.

## Example Service

When you create a service add the annotations and labels from the example below
and fill in your route53 DNSName and HostedZoneID.

```yaml
kind: Service
apiVersion: v1
metadata:
  name: my-service
  labels:
    route53: loadBalancer
  annotations:
    foolusion-aws-route53-dns-name: my.domain.com
    foolusion-aws-route53-hosted-zone: Z1089BC83EA983
  spec:
    type: LoadBalancer
    selector:
      app: MyApp
    ports:
      targetPort: 8080
```

PR's, Issues welcome.
