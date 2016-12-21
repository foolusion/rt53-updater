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

There are two environment variables you will need to configure. They are
`NAMESPACE` and `AWS_REGION`. `NAMESPACE` allows you to filter to only services
in the specified namespace. `AWS_REGION` is required for the query to
describe-load-balancers.

To Build and create deployment artifacts you can run `make`. If you just want
to build the go binary you can run `go build`. If you don't want to build at
all just use the `foolusion/rt53-updater` image from docker hub.

> If you run `make` be sure to change the tagBase to use your image repository.

To deploy take a look at `rt53-updater.yaml` or `kube.yaml` if you used `make`.
Then run `kubectl apply -f your-deployment-file.yaml`.

PR's, Issues welcome.
