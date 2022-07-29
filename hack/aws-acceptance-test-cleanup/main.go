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

	// Find VPCs to delete. Most resources we create belong to a VPC, except
	// for IAM resources, and so if there are no VPCs, that means all leftover resources have been deleted.
	var nextToken *string
	var toDeleteVPCs []*ec2.Vpc
	for {
		vpcsOutput, err := ec2Client.DescribeVpcsWithContext(ctx, &ec2.DescribeVpcsInput{
			NextToken: nextToken,
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("tag-key"),
					Values: []*string{aws.String(buildURLTag)},
				},
				{
					Name:   aws.String("tag:Name"),
					Values: []*string{aws.String("consul-k8s-*")},
				},
			},
		})
		if err != nil {
			return err
		}
		toDeleteVPCs = append(vpcsOutput.Vpcs)
		nextToken = vpcsOutput.NextToken
		if nextToken == nil {
			break
		}
	}

	if len(toDeleteVPCs) == 0 {
		fmt.Println("Found no VPCs or associated resources to clean up")
		return nil
	}

	var vpcPrint string
	for _, vpc := range toDeleteVPCs {
		vpcName, buildURL := vpcNameAndBuildURL(vpc)
		vpcPrint += fmt.Sprintf("- %s (%s)\n", vpcName, buildURL)
	}

	fmt.Printf("Found VPCs:\n%s", vpcPrint)

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
			fmt.Println("\nDo you want to delete these VPCs and associated resources including EKS clusters (y/n)?")
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

	// Find EKS clusters to delete.
	clusters, err := eksClient.ListClustersWithContext(ctx, &eks.ListClustersInput{})
	if err != nil {
		return err
	}
	toDeleteClusters := make(map[string]eks.Cluster)
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
				toDeleteClusters[*cluster] = *clusterData.Cluster
			}

		}
	}

	// Delete VPCs and associated resources.
	for _, vpc := range toDeleteVPCs {
		fmt.Printf("Deleting VPC and associated resources: %s\n", *vpc.VpcId)
		vpcName, _ := vpcNameAndBuildURL(vpc)
		cluster, ok := toDeleteClusters[vpcName]
		if !ok {
			fmt.Printf("Found no associated EKS cluster for VPC: %s\n", vpcName)
		} else {
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
		}

		vpcID := vpc.VpcId

		// Once we have the VPC ID, collect VPC peering connections to delete.
		filterNameAcceptor := "accepter-vpc-info.vpc-id"
		filterNameRequester := "requester-vpc-info.vpc-id"
		vpcPeeringConnectionsWithAcceptor, err := ec2Client.DescribeVpcPeeringConnections(&ec2.DescribeVpcPeeringConnectionsInput{
			Filters: []*ec2.Filter{
				{
					Name:   &filterNameAcceptor,
					Values: []*string{vpcID},
				},
			},
		})

		if err != nil {
			return err
		}
		vpcPeeringConnectionsWithRequester, err := ec2Client.DescribeVpcPeeringConnections(&ec2.DescribeVpcPeeringConnectionsInput{
			Filters: []*ec2.Filter{
				{
					Name:   &filterNameRequester,
					Values: []*string{vpcID},
				},
			},
		})
		vpcPeeringConnectionsToDelete := append(vpcPeeringConnectionsWithAcceptor.VpcPeeringConnections, vpcPeeringConnectionsWithRequester.VpcPeeringConnections...)

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
				// We only care about Nat gateways whose state is not "deleted."
				// Deleted Nat gateways will show in the output for about 1hr
				// (https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway.html#nat-gateway-deleting),
				// but we can proceed with deleting other resources once its state is deleted.
				currNatGateways, err := ec2Client.DescribeNatGatewaysWithContext(ctx, &ec2.DescribeNatGatewaysInput{
					Filter: []*ec2.Filter{
						{
							Name:   aws.String("vpc-id"),
							Values: []*string{vpcID},
						},
						{
							Name: aws.String("state"),
							Values: []*string{
								aws.String(ec2.NatGatewayStatePending),
								aws.String(ec2.NatGatewayStateFailed),
								aws.String(ec2.NatGatewayStateDeleting),
								aws.String(ec2.NatGatewayStateAvailable),
							},
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

			// Release Elastic IP associated with the NAT gateway (if any).
			for _, address := range gateway.NatGatewayAddresses {
				if address.AllocationId != nil {
					fmt.Printf("NAT gateway: Releasing Elastic IP... [id=%s]\n", *address.AllocationId)
					_, err := ec2Client.ReleaseAddressWithContext(ctx, &ec2.ReleaseAddressInput{AllocationId: address.AllocationId})
					if err != nil && !strings.Contains(err.Error(), "InvalidAllocationID.NotFound") {
						return err
					}
					fmt.Printf("NAT gateway: Elastic IP released [id=%s]\n", *address.AllocationId)
				}
			}
		}

		// Delete ELBs (usually left from mesh gateway tests).
		elbs, err := elbClient.DescribeLoadBalancersWithContext(ctx, &elb.DescribeLoadBalancersInput{})
		if err != nil {
			return err
		}
		for _, elbDescrip := range elbs.LoadBalancerDescriptions {
			if *elbDescrip.VPCId != *vpcID {
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
				if strings.Contains(err.Error(), elb.ErrCodeAccessPointNotFoundException) {
					return nil
				} else if err != nil {
					return err
				}
				if len(currELBs.LoadBalancerDescriptions) > 0 {
					return errNotDestroyed
				}

				return nil
			}); err != nil {
				return err
			}
			// Allow time for ELB deletion to propagate so that we can detach the internet gateway.
			time.Sleep(30 * time.Second)
			fmt.Printf("ELB: Destroyed [id=%s]\n", *elbDescrip.LoadBalancerName)
		}

		// Delete internet gateways.
		igws, err := ec2Client.DescribeInternetGatewaysWithContext(ctx, &ec2.DescribeInternetGatewaysInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("attachment.vpc-id"),
					Values: []*string{vpcID},
				},
			},
		})
		for _, igw := range igws.InternetGateways {
			fmt.Printf("Internet gateway: Detaching from VPC... [id=%s]\n", *igw.InternetGatewayId)
			_, err := ec2Client.DetachInternetGatewayWithContext(ctx, &ec2.DetachInternetGatewayInput{
				InternetGatewayId: igw.InternetGatewayId,
				VpcId:             vpcID,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Internet gateway: Detached [id=%s]\n", *igw.InternetGatewayId)

			fmt.Printf("Internet gateway: Destroying... [id=%s]\n", *igw.InternetGatewayId)
			_, err = ec2Client.DeleteInternetGatewayWithContext(ctx, &ec2.DeleteInternetGatewayInput{
				InternetGatewayId: igw.InternetGatewayId,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Internet gateway: Destroyed [id=%s]\n", *igw.InternetGatewayId)
		}

		// Delete subnets.
		subnets, err := ec2Client.DescribeSubnetsWithContext(ctx, &ec2.DescribeSubnetsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{vpcID},
				},
			},
		})
		for _, subnet := range subnets.Subnets {
			fmt.Printf("Subnet: Destroying... [id=%s]\n", *subnet.SubnetId)
			_, err := ec2Client.DeleteSubnetWithContext(ctx, &ec2.DeleteSubnetInput{
				SubnetId: subnet.SubnetId,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Subnet: Destroyed [id=%s]\n", *subnet.SubnetId)
		}

		// Delete route tables.
		routeTables, err := ec2Client.DescribeRouteTablesWithContext(ctx, &ec2.DescribeRouteTablesInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{vpcID},
				},
			},
		})
		for _, routeTable := range routeTables.RouteTables {
			// Find out if this is the main route table.
			var mainRouteTable bool
			for _, association := range routeTable.Associations {
				if association.Main != nil && *association.Main {
					mainRouteTable = true
					break
				}
			}

			if mainRouteTable {
				fmt.Printf("Route table: Skipping the main route table [id=%s]\n", *routeTable.RouteTableId)
			} else {
				fmt.Printf("Route table: Destroying... [id=%s]\n", *routeTable.RouteTableId)
				_, err := ec2Client.DeleteRouteTableWithContext(ctx, &ec2.DeleteRouteTableInput{
					RouteTableId: routeTable.RouteTableId,
				})
				if err != nil {
					return err
				}
				fmt.Printf("Route table: Destroyed [id=%s]\n", *routeTable.RouteTableId)
			}
		}

		// Delete security groups.
		sgs, err := ec2Client.DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{vpcID},
				},
			},
		})
		for _, sg := range sgs.SecurityGroups {
			if len(sg.IpPermissions) > 0 {
				revokeSGInput := &ec2.RevokeSecurityGroupIngressInput{GroupId: sg.GroupId}
				revokeSGInput.SetIpPermissions(sg.IpPermissions)
				fmt.Printf("Security group: Removing security group rules... [id=%s]\n", *sg.GroupId)
				_, err := ec2Client.RevokeSecurityGroupIngressWithContext(ctx, revokeSGInput)
				if err != nil {
					return err
				}
				fmt.Printf("Security group: Removed security group rules [id=%s]\n", *sg.GroupId)
			}
		}

		for _, sg := range sgs.SecurityGroups {
			if sg.GroupName != nil && *sg.GroupName == "default" {
				fmt.Printf("Security group: Skipping default security group [id=%s]\n", *sg.GroupId)
				continue
			}
			fmt.Printf("Security group: Destroying... [id=%s]\n", *sg.GroupId)
			_, err = ec2Client.DeleteSecurityGroupWithContext(ctx, &ec2.DeleteSecurityGroupInput{
				GroupId: sg.GroupId,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Security group: Destroyed [id=%s]\n", *sg.GroupId)
		}

		// Delete VPC Peering Connections.
		for _, vpcpc := range vpcPeeringConnectionsToDelete {
			_, err = ec2Client.DeleteVpcPeeringConnection(&ec2.DeleteVpcPeeringConnectionInput{VpcPeeringConnectionId: vpcpc.VpcPeeringConnectionId})
			if err != nil {
				return err
			}
			fmt.Printf("VPC PeeringConnection: Destroyed [id=%s]\n", *vpcpc.VpcPeeringConnectionId)
		}

		// Delete VPC. Sometimes there's a race condition where AWS thinks
		// the VPC still has dependencies but they've already been deleted so
		// we may need to retry a couple times.
		fmt.Printf("VPC: Destroying... [id=%s]\n", *vpcID)
		// Retry up to 10 times.
		retryCount := 0
		for ; retryCount < 10; retryCount++ {
			_, err = ec2Client.DeleteVpc(&ec2.DeleteVpcInput{
				VpcId: vpcID,
			})
			if err == nil {
				break
			}
			fmt.Printf("VPC: Destroy error... [id=%s,err=%q,retry=%d]\n", *vpcID, err, retryCount)
			time.Sleep(5 * time.Second)
		}
		if retryCount == 10 {
			return errors.New("reached max retry count deleting VPC")
		}

		fmt.Printf("VPC: Destroyed [id=%s]\n", *vpcID)
	}

	return nil
}

func vpcNameAndBuildURL(vpc *ec2.Vpc) (string, string) {
	var vpcName string
	var buildURL string
	for _, tag := range vpc.Tags {
		switch *tag.Key {
		case "Name":
			vpcName = *tag.Value
		case buildURLTag:
			buildURL = *tag.Value
		}
	}
	return vpcName, buildURL
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
