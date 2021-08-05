package main

// This script deletes AWS resources created for acceptance tests that have
// been left around after an acceptance test fails and is not cleaned up.
//
// Usage: go run main.go [-auto-approve]

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/cenkalti/backoff/v4"
)

const (
	// buildURLTag is a tag on AWS resources set by the acceptance tests.
	buildURLTag = "build_url"
)

var (
	flagAutoApprove bool
	errNotDestroyed = errors.New("not yet destroyed")
)

func main() {
	flag.BoolVar(&flagAutoApprove, "auto-approve", false, "Skip interactive approval before destroying.")
	flag.Parse()

	termChan := make(chan os.Signal)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		<-termChan
		fmt.Println("Received stop signal")
		cancelFunc()
	}()

	if err := realMain(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func realMain(ctx context.Context) error {
	// Create AWS clients.
	clientSession, err := session.NewSession()
	if err != nil {
		return err
	}
	awsCfg := &aws.Config{Region: aws.String("us-west-2")}
	eksClient := eks.New(clientSession, awsCfg)
	ec2Client := ec2.New(clientSession, awsCfg)
	elbClient := elb.New(clientSession, awsCfg)

	// Find clusters to delete.
	clusters, err := eksClient.ListClustersWithContext(ctx, &eks.ListClustersInput{})
	if err != nil {
		return err
	}
	var toDeleteClusters []eks.Cluster
	for _, cluster := range clusters.Clusters {
		if strings.HasPrefix(*cluster, "consul-k8s-") {
			// Check the tags of the cluster to ensure they're acceptance test clusters.
			clusterData, err := eksClient.DescribeClusterWithContext(ctx, &eks.DescribeClusterInput{
				Name: cluster,
			})
			if err != nil {
				return err
			}
			if _, ok := clusterData.Cluster.Tags[buildURLTag]; ok {
				toDeleteClusters = append(toDeleteClusters, *clusterData.Cluster)
			}

		}
	}

	var clusterPrint string
	for _, c := range toDeleteClusters {
		clusterPrint += fmt.Sprintf("- %s (%s)\n", *c.Name, *c.Tags[buildURLTag])
	}
	if len(toDeleteClusters) == 0 {
		fmt.Println("No EKS clusters to clean up")
		return nil
	}
	fmt.Printf("Found EKS clusters:\n%s", clusterPrint)

	// Check for approval.
	if !flagAutoApprove {
		type input struct {
			text string
			err  error
		}
		inputCh := make(chan input)

		// Read input in a goroutine so we can also exit if we get a Ctrl-C
		// (see select{} below).
		go func() {
			reader := bufio.NewReader(os.Stdin)
			fmt.Println("\nDo you want to delete these clusters and associated resources including VPCs (y/n)?")
			inputStr, err := reader.ReadString('\n')
			if err != nil {
				inputCh <- input{err: err}
				return
			}
			inputCh <- input{text: inputStr}
		}()

		select {
		case in := <-inputCh:
			if in.err != nil {
				return in.err
			}
			inputTrimmed := strings.TrimSpace(in.text)
			if inputTrimmed != "y" && inputTrimmed != "yes" {
				return errors.New("exiting after negative")
			}
		case <-ctx.Done():
			return errors.New("context cancelled")
		}
	}

	// Delete clusters and associated resources.
	for _, cluster := range toDeleteClusters {

		// Delete node groups.
		nodeGroups, err := eksClient.ListNodegroupsWithContext(ctx, &eks.ListNodegroupsInput{
			ClusterName: cluster.Name,
		})
		if err != nil {
			return err
		}
		for _, groupID := range nodeGroups.Nodegroups {
			fmt.Printf("Node group: Destroying... [id=%s]\n", *groupID)
			_, err = eksClient.DeleteNodegroupWithContext(ctx, &eks.DeleteNodegroupInput{
				ClusterName:   cluster.Name,
				NodegroupName: groupID,
			})
			if err != nil {
				return err
			}

			// Wait for node group to be deleted.
			if err := destroyBackoff(ctx, "Node group", *groupID, func() error {
				currNodeGroups, err := eksClient.ListNodegroupsWithContext(ctx, &eks.ListNodegroupsInput{
					ClusterName: cluster.Name,
				})
				if err != nil {
					return err
				}
				for _, currGroup := range currNodeGroups.Nodegroups {
					if *currGroup == *groupID {
						return errNotDestroyed
					}
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("Node group: Destroyed [id=%s]\n", *groupID)
		}

		// Delete cluster.
		fmt.Printf("EKS cluster: Destroying... [id=%s]\n", *cluster.Name)
		_, err = eksClient.DeleteClusterWithContext(ctx, &eks.DeleteClusterInput{Name: cluster.Name})
		if err != nil {
			return err
		}
		if err := destroyBackoff(ctx, "EKS cluster", *cluster.Name, func() error {
			currClusters, err := eksClient.ListClustersWithContext(ctx, &eks.ListClustersInput{})
			if err != nil {
				return err
			}
			for _, currCluster := range currClusters.Clusters {
				if *currCluster == *cluster.Name {
					return errNotDestroyed
				}
			}
			return nil
		}); err != nil {
			return err
		}
		fmt.Printf("EKS cluster: Destroyed [id=%s]\n", *cluster.Name)

		vpcID := cluster.ResourcesVpcConfig.VpcId

		// Delete NAT gateways.
		natGateways, err := ec2Client.DescribeNatGatewaysWithContext(ctx, &ec2.DescribeNatGatewaysInput{
			Filter: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{vpcID},
				},
			},
		})
		if err != nil {
			return err
		}
		for _, gateway := range natGateways.NatGateways {
			fmt.Printf("NAT gateway: Destroying... [id=%s]\n", *gateway.NatGatewayId)
			_, err = ec2Client.DeleteNatGatewayWithContext(ctx, &ec2.DeleteNatGatewayInput{
				NatGatewayId: gateway.NatGatewayId,
			})
			if err != nil {
				return err
			}

			if err := destroyBackoff(ctx, "NAT gateway", *gateway.NatGatewayId, func() error {
				currNatGateways, err := ec2Client.DescribeNatGatewaysWithContext(ctx, &ec2.DescribeNatGatewaysInput{
					Filter: []*ec2.Filter{
						{
							Name:   aws.String("vpc-id"),
							Values: []*string{vpcID},
						},
					},
				})
				if err != nil {
					return err
				}
				if len(currNatGateways.NatGateways) > 0 {
					return errNotDestroyed
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("NAT gateway: Destroyed [id=%s]\n", *gateway.NatGatewayId)
		}

		// Delete ELBs (usually left from mesh gateway tests).
		elbs, err := elbClient.DescribeLoadBalancersWithContext(ctx, &elb.DescribeLoadBalancersInput{})
		if err != nil {
			return err
		}
		for _, elbDescrip := range elbs.LoadBalancerDescriptions {
			if elbDescrip.VPCId != vpcID {
				continue
			}

			fmt.Printf("ELB: Destroying... [id=%s]\n", *elbDescrip.LoadBalancerName)

			_, err = elbClient.DeleteLoadBalancerWithContext(ctx, &elb.DeleteLoadBalancerInput{
				LoadBalancerName: elbDescrip.LoadBalancerName,
			})
			if err != nil {
				return err
			}

			if err := destroyBackoff(ctx, "ELB", *elbDescrip.LoadBalancerName, func() error {
				currELBs, err := elbClient.DescribeLoadBalancersWithContext(ctx, &elb.DescribeLoadBalancersInput{
					LoadBalancerNames: []*string{elbDescrip.LoadBalancerName},
				})
				if err != nil {
					return err
				}
				if len(currELBs.LoadBalancerDescriptions) > 0 {
					return errNotDestroyed
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("ELB: Destroyed [id=%s]\n", *elbDescrip.LoadBalancerName)
		}

		// Delete VPC.
		fmt.Printf("VPC: Destroying... [id=%s]\n", *vpcID)
		_, err = ec2Client.DeleteVpc(&ec2.DeleteVpcInput{
			VpcId: vpcID,
		})
		if err != nil {
			return err
		}
		if err := destroyBackoff(ctx, "VPC", *vpcID, func() error {
			currVPCs, err := ec2Client.DescribeVpcsWithContext(ctx, &ec2.DescribeVpcsInput{
				VpcIds: []*string{vpcID},
			})
			if err != nil {
				return err
			}
			if len(currVPCs.Vpcs) > 0 {
				return errNotDestroyed
			}
			return nil
		}); err != nil {
			return err
		}
		fmt.Printf("VPC: Destroyed [id=%s]\n", *vpcID)
	}

	return nil
}

// destroyBackoff runs destroyF in a backoff loop. It logs each loop.
func destroyBackoff(ctx context.Context, resourceKind string, resourceID string, destroyF func() error) error {
	start := time.Now()
	expoBackoff := backoff.NewExponentialBackOff()
	// NAT gateways take forever to destroy.
	expoBackoff.MaxElapsedTime = 1*time.Hour + 30*time.Minute

	return backoff.Retry(func() error {
		err := destroyF()
		if err != nil {
			var errLog string
			if err != errNotDestroyed {
				errLog = fmt.Sprintf(" err=%q,", err)
			}
			fmt.Printf("%s: Still destroying... [id=%s,%s %s elapsed]\n", resourceKind, resourceID, errLog, time.Since(start).Round(time.Second))
		}
		return err
	}, backoff.WithContext(expoBackoff, ctx))
}
