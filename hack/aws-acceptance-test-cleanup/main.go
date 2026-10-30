// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	"github.com/cenkalti/backoff/v4"
)

const (
	// buildURLTag is a tag on AWS resources set by the acceptance tests.
	buildURLTag = "build_url"

	// Generic not-found codes across services.
	iamErrCodeNoSuchEntity              = "NoSuchEntity"
	eksErrCodeResourceNotFoundException = "ResourceNotFoundException"
	elbErrCodeAccessPointNotFound       = "AccessPointNotFound"
	elbErrCodeLoadBalancerNotFound      = "LoadBalancerNotFound"

	// Known EC2 not-found codes treated as terminal success during cleanup races.
	ec2ErrCodeInvalidAllocationIDNotFound       = "InvalidAllocationID.NotFound"
	ec2ErrCodeInvalidInternetGatewayIDNotFound  = "InvalidInternetGatewayID.NotFound"
	ec2ErrCodeInvalidNatGatewayIDNotFound       = "InvalidNatGatewayID.NotFound"
	ec2ErrCodeInvalidNetworkInterfaceIDNotFound = "InvalidNetworkInterfaceID.NotFound"
	ec2ErrCodeInvalidRouteTableIDNotFound       = "InvalidRouteTableID.NotFound"
	ec2ErrCodeInvalidSubnetIDNotFound           = "InvalidSubnetID.NotFound"
	ec2ErrCodeInvalidGroupNotFound              = "InvalidGroup.NotFound"
	ec2ErrCodeInvalidVolumeNotFound             = "InvalidVolume.NotFound"
	ec2ErrCodeInvalidVpcPeeringConnNotFound     = "InvalidVpcPeeringConnectionID.NotFound"
	ec2ErrCodeInvalidVpcIDNotFound              = "InvalidVpcID.NotFound"
)

var (
	flagAutoApprove bool
	errNotDestroyed = errors.New("not yet destroyed")
)

type oidcProvider struct {
	arn      string
	buildURL string
}

func main() {
	flag.BoolVar(&flagAutoApprove, "auto-approve", false, "Skip interactive approval before destroying.")
	flag.Parse()

	// Buffered so the sender is never blocked if we exit before reading.
	termChan := make(chan os.Signal, 1)
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
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-west-2"))
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}

	eksClient := eks.NewFromConfig(cfg)
	ec2Client := ec2.NewFromConfig(cfg)
	elbClient := elasticloadbalancing.NewFromConfig(cfg)
	iamClient := iam.NewFromConfig(cfg)

	// Find volumes and delete.
	if err := cleanupPersistentVolumes(ctx, ec2Client); err != nil {
		return err
	}

	// Find IAM roles and delete.
	if err := cleanupIAMRoles(ctx, iamClient); err != nil {
		return err
	}

	// Find IAM policies and delete.
	if err := cleanupIAMPolicies(ctx, iamClient); err != nil {
		return err
	}

	// Find OIDC providers to delete.
	oidcOut, err := iamClient.ListOpenIDConnectProviders(ctx, &iam.ListOpenIDConnectProvidersInput{})
	if err != nil {
		return err
	}

	var toDeleteOIDCProviders []oidcProvider
	for _, entry := range oidcOut.OpenIDConnectProviderList {
		arnStr := aws.ToString(entry.Arn)
		older, err := oidcOlderThanEightHours(ctx, iamClient, entry.Arn)
		if err != nil {
			return err
		}
		if !older {
			fmt.Printf("Skipping OIDC provider: %s because it's not over 8 hours old\n", arnStr)
			continue
		}
		tagsOut, err := iamClient.ListOpenIDConnectProviderTags(ctx, &iam.ListOpenIDConnectProviderTagsInput{
			OpenIDConnectProviderArn: entry.Arn,
		})
		if err != nil {
			return err
		}
		for _, tag := range tagsOut.Tags {
			if aws.ToString(tag.Key) == buildURLTag {
				toDeleteOIDCProviders = append(toDeleteOIDCProviders, oidcProvider{
					arn:      arnStr,
					buildURL: aws.ToString(tag.Value),
				})
			}
		}
	}

	if len(toDeleteOIDCProviders) == 0 {
		fmt.Println("Found no OIDC Providers to clean up")
	} else {
		var oidcPrint string
		for _, p := range toDeleteOIDCProviders {
			oidcPrint += fmt.Sprintf("- %s (%s)\n", p.arn, p.buildURL)
		}
		fmt.Printf("Found OIDC Providers:\n%s", oidcPrint)

		if !flagAutoApprove {
			if err := promptApproval(ctx, "Do you want to delete these OIDC Providers (y/n)?"); err != nil {
				return err
			}
		}

		for _, p := range toDeleteOIDCProviders {
			fmt.Printf("Deleting OIDC provider: %s\n", p.arn)
			_, err := iamClient.DeleteOpenIDConnectProvider(ctx, &iam.DeleteOpenIDConnectProviderInput{
				OpenIDConnectProviderArn: aws.String(p.arn),
			})
			if err != nil {
				if awsErrCodeIs(err, iamErrCodeNoSuchEntity) {
					fmt.Printf("OIDC provider: Not found (already destroyed) [id=%s]\n", p.arn)
					continue
				}
				return err
			}
		}
	}

	// Find VPCs to delete. Most resources we create belong to a VPC, except
	// for IAM resources, so if there are no VPCs all leftover resources have been deleted.
	var toDeleteVPCs []ec2types.Vpc
	vpcPaginator := ec2.NewDescribeVpcsPaginator(ec2Client, &ec2.DescribeVpcsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag-key"),
				Values: []string{buildURLTag},
			},
			{
				Name:   aws.String("tag:Name"),
				Values: []string{"consul-k8s-*"},
			},
		},
	})
	for vpcPaginator.HasMorePages() {
		page, err := vpcPaginator.NextPage(ctx)
		if err != nil {
			return err
		}
		toDeleteVPCs = append(toDeleteVPCs, page.Vpcs...)
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

	if !flagAutoApprove {
		if err := promptApproval(ctx, "Do you want to delete these VPCs and associated resources including EKS clusters (y/n)?"); err != nil {
			return err
		}
	}

	// Find EKS clusters to delete.
	toDeleteClusters := make(map[string]ekstypes.Cluster)
	clusterPaginator := eks.NewListClustersPaginator(eksClient, &eks.ListClustersInput{})
	for clusterPaginator.HasMorePages() {
		page, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, clusterName := range page.Clusters {
			if !strings.HasPrefix(clusterName, "consul-k8s-") {
				continue
			}
			// Check the tags of the cluster to ensure they're acceptance test clusters.
			clusterData, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
				Name: aws.String(clusterName),
			})
			if err != nil {
				// Cluster can disappear between list/describe calls during concurrent cleanup.
				if awsErrCodeIs(err, eksErrCodeResourceNotFoundException) {
					fmt.Printf("EKS cluster: Not found (already deleted) [id=%s]\n", clusterName)
					continue
				}
				return err
			}
			if clusterData.Cluster == nil {
				continue
			}
			if _, ok := clusterData.Cluster.Tags[buildURLTag]; ok {
				toDeleteClusters[clusterName] = *clusterData.Cluster
			}
		}
	}

	// Delete VPCs and associated resources.
	for _, vpc := range toDeleteVPCs {
		vpcID := vpc.VpcId
		vpcIDStr := aws.ToString(vpcID)
		vpcName, _ := vpcNameAndBuildURL(vpc)

		fmt.Printf("Deleting VPC and associated resources: %s\n", vpcIDStr)

		cluster, ok := toDeleteClusters[vpcName]
		if !ok {
			fmt.Printf("Found no associated EKS cluster for VPC: %s\n", vpcName)
		} else {
			// Delete node groups.
			ngPaginator := eks.NewListNodegroupsPaginator(eksClient, &eks.ListNodegroupsInput{
				ClusterName: cluster.Name,
			})
			for ngPaginator.HasMorePages() {
				page, err := ngPaginator.NextPage(ctx)
				if err != nil {
					if awsErrCodeIs(err, eksErrCodeResourceNotFoundException) {
						fmt.Printf("EKS cluster: Not found while listing node groups (already deleted) [id=%s]\n", aws.ToString(cluster.Name))
						break
					}
					return err
				}
				for _, groupID := range page.Nodegroups {
					fmt.Printf("Node group: Destroying... [id=%s]\n", groupID)
					_, err = eksClient.DeleteNodegroup(ctx, &eks.DeleteNodegroupInput{
						ClusterName:   cluster.Name,
						NodegroupName: aws.String(groupID),
					})
					if err != nil {
						if awsErrCodeIs(err, eksErrCodeResourceNotFoundException) {
							fmt.Printf("Node group: Not found (already destroyed) [id=%s]\n", groupID)
							continue
						}
						return err
					}

					// Wait for node group to be deleted.
					if err := destroyBackoff(ctx, "Node group", groupID, func() error {
						out, err := eksClient.ListNodegroups(ctx, &eks.ListNodegroupsInput{
							ClusterName: cluster.Name,
						})
						if err != nil {
							if awsErrCodeIs(err, eksErrCodeResourceNotFoundException) {
								// Cluster is already gone, so node groups are gone too.
								return nil
							}
							return err
						}
						for _, ng := range out.Nodegroups {
							if ng == groupID {
								return errNotDestroyed
							}
						}
						return nil
					}); err != nil {
						return err
					}
					fmt.Printf("Node group: Destroyed [id=%s]\n", groupID)
				}
			}

			// Delete cluster.
			clusterName := aws.ToString(cluster.Name)
			fmt.Printf("EKS cluster: Destroying... [id=%s]\n", clusterName)
			_, err = eksClient.DeleteCluster(ctx, &eks.DeleteClusterInput{Name: cluster.Name})
			switch {
			case awsErrCodeIs(err, eksErrCodeResourceNotFoundException):
				fmt.Printf("EKS cluster: Not found (already destroyed) [id=%s]\n", clusterName)
			case err != nil:
				return err
			default:
				if err := destroyBackoff(ctx, "EKS cluster", clusterName, func() error {
					out, err := eksClient.ListClusters(ctx, &eks.ListClustersInput{})
					if err != nil {
						return err
					}
					for _, c := range out.Clusters {
						if c == clusterName {
							return errNotDestroyed
						}
					}
					return nil
				}); err != nil {
					return err
				}
				fmt.Printf("EKS cluster: Destroyed [id=%s]\n", clusterName)
			}
		}

		// Collect VPC peering connections to delete.
		var vpcPeeringConnsToDelete []ec2types.VpcPeeringConnection
		for _, filterName := range []string{"accepter-vpc-info.vpc-id", "requester-vpc-info.vpc-id"} {
			out, err := ec2Client.DescribeVpcPeeringConnections(ctx, &ec2.DescribeVpcPeeringConnectionsInput{
				Filters: []ec2types.Filter{
					{Name: aws.String(filterName), Values: []string{vpcIDStr}},
				},
			})
			if err != nil {
				if awsErrCodeIs(err, ec2ErrCodeInvalidVpcIDNotFound) {
					fmt.Printf("VPC peering connections: VPC not found (already destroyed) [id=%s]\n", vpcIDStr)
					continue
				}
				return err
			}
			vpcPeeringConnsToDelete = append(vpcPeeringConnsToDelete, out.VpcPeeringConnections...)
		}

		// Delete NAT gateways.
		natGWsOut, err := ec2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
			Filter: []ec2types.Filter{
				{Name: aws.String("vpc-id"), Values: []string{vpcIDStr}},
			},
		})
		if err != nil {
			return err
		}

		for _, gateway := range natGWsOut.NatGateways {
			gwID := aws.ToString(gateway.NatGatewayId)

			// releaseEIPs releases any Elastic IPs allocated to this NAT gateway.
			// Run regardless of whether the NAT gateway itself was already gone, since
			// we still have the address list from the earlier DescribeNatGateways call.
			releaseEIPs := func() error {
				for _, address := range gateway.NatGatewayAddresses {
					if address.AllocationId == nil {
						continue
					}
					allocID := aws.ToString(address.AllocationId)
					fmt.Printf("NAT gateway: Releasing Elastic IP... [id=%s]\n", allocID)
					_, err := ec2Client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
						AllocationId: address.AllocationId,
					})
					if err != nil && !awsErrCodeIs(err, ec2ErrCodeInvalidAllocationIDNotFound) {
						return err
					}
					fmt.Printf("NAT gateway: Elastic IP released [id=%s]\n", allocID)
				}
				return nil
			}

			fmt.Printf("NAT gateway: Destroying... [id=%s]\n", gwID)
			_, err = ec2Client.DeleteNatGateway(ctx, &ec2.DeleteNatGatewayInput{
				NatGatewayId: gateway.NatGatewayId,
			})
			if err != nil {
				if awsErrCodeIs(err, ec2ErrCodeInvalidNatGatewayIDNotFound) {
					fmt.Printf("NAT gateway: Not found (already destroyed) [id=%s]\n", gwID)
					if err := releaseEIPs(); err != nil {
						return err
					}
					continue
				}
				return err
			}

			if err := destroyBackoff(ctx, "NAT gateway", gwID, func() error {
				// We only care about Nat gateways whose state is not "deleted."
				// Deleted Nat gateways will show in the output for about 1hr
				// (https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway.html#nat-gateway-deleting),
				// but we can proceed with deleting other resources once its state is deleted.
				out, err := ec2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
					Filter: []ec2types.Filter{
						{Name: aws.String("vpc-id"), Values: []string{vpcIDStr}},
						{
							Name: aws.String("state"),
							Values: []string{
								string(ec2types.NatGatewayStatePending),
								string(ec2types.NatGatewayStateFailed),
								string(ec2types.NatGatewayStateDeleting),
								string(ec2types.NatGatewayStateAvailable),
							},
						},
					},
				})
				if err != nil {
					if awsErrCodeIs(err, ec2ErrCodeInvalidNatGatewayIDNotFound) {
						return nil
					}
					return err
				}
				if len(out.NatGateways) > 0 {
					return errNotDestroyed
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("NAT gateway: Destroyed [id=%s]\n", gwID)

			if err := releaseEIPs(); err != nil {
				return err
			}
		}

		// Delete ELBs (usually left from mesh gateway tests).
		elbsOut, err := elbClient.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{})
		if err != nil {
			return err
		}
		for _, lb := range elbsOut.LoadBalancerDescriptions {
			if aws.ToString(lb.VPCId) != vpcIDStr {
				continue
			}
			lbName := aws.ToString(lb.LoadBalancerName)
			fmt.Printf("ELB: Destroying... [id=%s]\n", lbName)
			_, err = elbClient.DeleteLoadBalancer(ctx, &elasticloadbalancing.DeleteLoadBalancerInput{
				LoadBalancerName: lb.LoadBalancerName,
			})
			if err != nil {
				if awsErrCodeIs(err, elbErrCodeAccessPointNotFound) || awsErrCodeIs(err, elbErrCodeLoadBalancerNotFound) {
					fmt.Printf("ELB: Not found (already destroyed) [id=%s]\n", lbName)
					continue
				}
				return err
			}

			if err := destroyBackoff(ctx, "ELB", lbName, func() error {
				currELBs, err := elbClient.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{
					LoadBalancerNames: []string{lbName},
				})
				if err != nil {
					// Classic ELB returns either code when the LB is gone.
					if awsErrCodeIs(err, elbErrCodeAccessPointNotFound) || awsErrCodeIs(err, elbErrCodeLoadBalancerNotFound) {
						return nil
					}
					return err
				}
				if len(currELBs.LoadBalancerDescriptions) > 0 {
					return errNotDestroyed
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("ELB: Destroyed [id=%s]\n", lbName)
		}

		// Delete internet gateways.
		igwsOut, err := ec2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("attachment.vpc-id"), Values: []string{vpcIDStr}},
			},
		})
		if err != nil {
			return err
		}

		for _, igw := range igwsOut.InternetGateways {
			igwID := aws.ToString(igw.InternetGatewayId)
			fmt.Printf("Internet gateway: Detaching from VPC... [id=%s]\n", igwID)
			detached := true
			if err := destroyBackoff(ctx, "Internet Gateway", igwID, func() error {
				_, err := ec2Client.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
					InternetGatewayId: igw.InternetGatewayId,
					VpcId:             vpcID,
				})
				if err != nil {
					if awsErrCodeIs(err, ec2ErrCodeInvalidInternetGatewayIDNotFound) {
						fmt.Printf("Internet gateway: Not found (already detached) [id=%s]\n", igwID)
						detached = false
						return nil
					}
					return err
				}
				return nil
			}); err != nil {
				return err
			}
			if detached {
				fmt.Printf("Internet gateway: Detached [id=%s]\n", igwID)
			}

			fmt.Printf("Internet gateway: Destroying... [id=%s]\n", igwID)
			_, err = ec2Client.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{
				InternetGatewayId: igw.InternetGatewayId,
			})
			switch {
			case awsErrCodeIs(err, ec2ErrCodeInvalidInternetGatewayIDNotFound):
				fmt.Printf("Internet gateway: Not found (already destroyed) [id=%s]\n", igwID)
			case err != nil:
				return err
			default:
				fmt.Printf("Internet gateway: Destroyed [id=%s]\n", igwID)
			}
		}

		// Delete network interfaces.
		nicsOut, err := ec2Client.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("vpc-id"), Values: []string{vpcIDStr}},
			},
		})
		if err != nil {
			return err
		}

		for _, nic := range nicsOut.NetworkInterfaces {
			nicID := aws.ToString(nic.NetworkInterfaceId)
			fmt.Printf("Network Interface: Destroying... [id=%s]\n", nicID)
			if err := destroyBackoff(ctx, "Network Interface", nicID, func() error {
				_, err := ec2Client.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
					NetworkInterfaceId: nic.NetworkInterfaceId,
				})
				if err != nil {
					if awsErrCodeIs(err, ec2ErrCodeInvalidNetworkInterfaceIDNotFound) {
						fmt.Printf("Network interface: Not found (already destroyed) [id=%s]\n", nicID)
						return nil
					}
					return err
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("Network interface: Destroyed [id=%s]\n", nicID)
		}

		// Delete subnets.
		subnetsOut, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("vpc-id"), Values: []string{vpcIDStr}},
			},
		})
		if err != nil {
			return err
		}

		for _, subnet := range subnetsOut.Subnets {
			subnetID := aws.ToString(subnet.SubnetId)
			fmt.Printf("Subnet: Destroying... [id=%s]\n", subnetID)
			if err := destroyBackoff(ctx, "Subnet", subnetID, func() error {
				_, err := ec2Client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
					SubnetId: subnet.SubnetId,
				})
				if err != nil {
					if awsErrCodeIs(err, ec2ErrCodeInvalidSubnetIDNotFound) {
						fmt.Printf("Subnet: Not found (already destroyed) [id=%s]\n", subnetID)
						return nil
					}
					return err
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("Subnet: Destroyed [id=%s]\n", subnetID)
		}

		// Delete route tables.
		rtOut, err := ec2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("vpc-id"), Values: []string{vpcIDStr}},
			},
		})
		if err != nil {
			return err
		}

		for _, rt := range rtOut.RouteTables {
			rtID := aws.ToString(rt.RouteTableId)
			var isMain bool
			for _, assoc := range rt.Associations {
				if aws.ToBool(assoc.Main) {
					isMain = true
					break
				}
			}
			if isMain {
				fmt.Printf("Route table: Skipping the main route table [id=%s]\n", rtID)
				continue
			}
			fmt.Printf("Route table: Destroying... [id=%s]\n", rtID)
			if err := destroyBackoff(ctx, "Route table", rtID, func() error {
				_, err := ec2Client.DeleteRouteTable(ctx, &ec2.DeleteRouteTableInput{
					RouteTableId: rt.RouteTableId,
				})
				if err != nil {
					if awsErrCodeIs(err, ec2ErrCodeInvalidRouteTableIDNotFound) {
						fmt.Printf("Route table: Not found (already destroyed) [id=%s]\n", rtID)
						return nil
					}
					return err
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("Route table: Destroyed [id=%s]\n", rtID)
		}

		// Delete security groups.
		sgsOut, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("vpc-id"), Values: []string{vpcIDStr}},
			},
		})
		if err != nil {
			return err
		}

		for _, sg := range sgsOut.SecurityGroups {
			if len(sg.IpPermissions) > 0 {
				sgID := aws.ToString(sg.GroupId)
				fmt.Printf("Security group: Removing security group rules... [id=%s]\n", sgID)
				_, err := ec2Client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
					GroupId:       sg.GroupId,
					IpPermissions: sg.IpPermissions,
				})
				if err != nil {
					if awsErrCodeIs(err, ec2ErrCodeInvalidGroupNotFound) {
						fmt.Printf("Security group: Not found while removing rules (already destroyed) [id=%s]\n", sgID)
						continue
					}
					return err
				}
				fmt.Printf("Security group: Removed security group rules [id=%s]\n", sgID)
			}
		}

		for _, sg := range sgsOut.SecurityGroups {
			if aws.ToString(sg.GroupName) == "default" {
				fmt.Printf("Security group: Skipping default security group [id=%s]\n", aws.ToString(sg.GroupId))
				continue
			}
			sgID := aws.ToString(sg.GroupId)
			fmt.Printf("Security group: Destroying... [id=%s]\n", sgID)
			if err := destroyBackoff(ctx, "Security group", sgID, func() error {
				_, err := ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
					GroupId: sg.GroupId,
				})
				if err != nil {
					if awsErrCodeIs(err, ec2ErrCodeInvalidGroupNotFound) {
						fmt.Printf("Security group: Not found (already destroyed) [id=%s]\n", sgID)
						return nil
					}
					return err
				}
				return nil
			}); err != nil {
				return err
			}
			fmt.Printf("Security group: Destroyed [id=%s]\n", sgID)
		}

		// Delete VPC peering connections.
		for _, conn := range vpcPeeringConnsToDelete {
			connID := aws.ToString(conn.VpcPeeringConnectionId)
			_, err = ec2Client.DeleteVpcPeeringConnection(ctx, &ec2.DeleteVpcPeeringConnectionInput{
				VpcPeeringConnectionId: conn.VpcPeeringConnectionId,
			})
			if err != nil {
				if awsErrCodeIs(err, ec2ErrCodeInvalidVpcPeeringConnNotFound) {
					fmt.Printf("VPC PeeringConnection: Not found (already destroyed) [id=%s]\n", connID)
					continue
				}
				return err
			}
			fmt.Printf("VPC PeeringConnection: Destroyed [id=%s]\n", connID)
		}

		// Delete VPC. Sometimes there's a race condition where AWS thinks
		// the VPC still has dependencies but they've already been deleted so
		// we may need to retry a couple times.
		fmt.Printf("VPC: Destroying... [id=%s]\n", vpcIDStr)
		var deleteVPCErr error
		for retryCount := 0; retryCount < 10; retryCount++ {
			_, deleteVPCErr = ec2Client.DeleteVpc(ctx, &ec2.DeleteVpcInput{VpcId: vpcID})
			if deleteVPCErr == nil {
				break
			}
			if awsErrCodeIs(deleteVPCErr, ec2ErrCodeInvalidVpcIDNotFound) {
				fmt.Printf("VPC: Not found (already destroyed) [id=%s]\n", vpcIDStr)
				deleteVPCErr = nil
				break
			}
			fmt.Printf("VPC: Destroy error... [id=%s,err=%q,retry=%d]\n", vpcIDStr, deleteVPCErr, retryCount)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
		if deleteVPCErr != nil {
			return errors.New("reached max retry count deleting VPC")
		}
		fmt.Printf("VPC: Destroyed [id=%s]\n", vpcIDStr)
	}

	return nil
}

// promptApproval reads a y/n answer from stdin, honouring context cancellation.
func promptApproval(ctx context.Context, prompt string) error {
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		fmt.Println("\n" + prompt)
		text, err := reader.ReadString('\n')
		ch <- result{text: text, err: err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if t := strings.TrimSpace(r.text); t != "y" && t != "yes" {
			return errors.New("exiting after negative")
		}
		return nil
	case <-ctx.Done():
		return errors.New("context cancelled")
	}
}

// oidcOlderThanEightHours checks if the OIDC provider is older than 8 hours.
func oidcOlderThanEightHours(ctx context.Context, iamClient *iam.Client, oidcArn *string) (bool, error) {
	out, err := iamClient.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: oidcArn,
	})
	if err != nil {
		if awsErrCodeIs(err, iamErrCodeNoSuchEntity) {
			return false, nil
		}
		return false, err
	}
	if out.CreateDate != nil && time.Since(*out.CreateDate).Hours() > 8 {
		return true, nil
	}
	return false, nil
}

func vpcNameAndBuildURL(vpc ec2types.Vpc) (string, string) {
	var vpcName, buildURL string
	for _, tag := range vpc.Tags {
		switch aws.ToString(tag.Key) {
		case "Name":
			vpcName = aws.ToString(tag.Value)
		case buildURLTag:
			buildURL = aws.ToString(tag.Value)
		}
	}
	return vpcName, buildURL
}

// destroyBackoff runs destroyF in a backoff loop. It logs each iteration.
func destroyBackoff(ctx context.Context, resourceKind, resourceID string, destroyF func() error) error {
	start := time.Now()
	expoBackoff := backoff.NewExponentialBackOff()
	// NAT gateways take forever to destroy.
	expoBackoff.MaxElapsedTime = 1*time.Hour + 30*time.Minute

	return backoff.Retry(func() error {
		err := destroyF()
		if err != nil {
			errLog := ""
			if !errors.Is(err, errNotDestroyed) {
				errLog = fmt.Sprintf(" err=%q,", err)
			}
			fmt.Printf("%s: Still destroying... [id=%s,%s %s elapsed]\n", resourceKind, resourceID, errLog, time.Since(start).Round(time.Second))
		}
		return err
	}, backoff.WithContext(expoBackoff, ctx))
}

func cleanupIAMRoles(ctx context.Context, iamClient *iam.Client) error {
	roles, err := filterIAMRolesWithPrefix(ctx, iamClient, "consul-k8s-")
	if err != nil {
		return fmt.Errorf("failed to list roles: %w", err)
	}

	if len(roles) == 0 {
		fmt.Println("Found no iamRoles to clean up")
		return nil
	}
	fmt.Printf("Found %d IAM roles to clean up\n", len(roles))

	for _, role := range roles {
		roleName := aws.ToString(role.RoleName)
		if err := detachRolePolicies(ctx, iamClient, role.RoleName); err != nil {
			fmt.Printf("Failed to detach policies for role %s: %v\n", roleName, err)
			continue
		}
		if err := removeRoleFromInstanceProfiles(ctx, iamClient, role.RoleName); err != nil {
			fmt.Printf("Failed to remove role %s from instance profiles: %v\n", roleName, err)
			continue
		}
		_, err = iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: role.RoleName})
		if err != nil {
			if awsErrCodeIs(err, iamErrCodeNoSuchEntity) {
				fmt.Printf("Role: Not found (already destroyed) [id=%s]\n", roleName)
			} else {
				fmt.Printf("Failed to delete role %s: %v\n", roleName, err)
			}
		} else {
			fmt.Printf("Deleted role: %s\n", roleName)
		}
	}

	return nil
}

func cleanupIAMPolicies(ctx context.Context, iamClient *iam.Client) error {
	var policiesToDelete []iamtypes.Policy

	paginator := iam.NewListPoliciesPaginator(iamClient, &iam.ListPoliciesInput{
		Scope: iamtypes.PolicyScopeTypeLocal,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list policies: %w", err)
		}
		for _, policy := range page.Policies {
			if strings.HasPrefix(aws.ToString(policy.PolicyName), "consul-k8s-") {
				policiesToDelete = append(policiesToDelete, policy)
			}
		}
	}

	if len(policiesToDelete) == 0 {
		fmt.Println("Found no IAM policies to clean up")
		return nil
	}
	fmt.Printf("Found %d IAM policies to clean up\n", len(policiesToDelete))

	for _, policy := range policiesToDelete {
		policyName := aws.ToString(policy.PolicyName)

		// First, list and delete non-default versions, as AWS requires before deleting the policy itself.
		versionsOut, err := iamClient.ListPolicyVersions(ctx, &iam.ListPolicyVersionsInput{
			PolicyArn: policy.Arn,
		})
		if err != nil {
			fmt.Printf("Failed to list versions for policy %s: %v\n", policyName, err)
			continue
		}

		// AWS IAM policy cannot be deleted while any non-default version still exists.
		// Skip deleting the policy in this run if any required version deletion fails.
		canDelete := true
		for _, version := range versionsOut.Versions {
			if !version.IsDefaultVersion {
				_, err := iamClient.DeletePolicyVersion(ctx, &iam.DeletePolicyVersionInput{
					PolicyArn: policy.Arn,
					VersionId: version.VersionId,
				})
				if err != nil {
					if awsErrCodeIs(err, iamErrCodeNoSuchEntity) {
						fmt.Printf("Policy version: Not found (already destroyed) [policy=%s,version=%s]\n", policyName, aws.ToString(version.VersionId))
					} else {
						fmt.Printf("Failed to delete non-default policy version %s for policy %s: %v\n", aws.ToString(version.VersionId), policyName, err)
						canDelete = false
					}
				}
			}
		}

		if !canDelete {
			fmt.Printf("Skipping deletion of policy %s because one or more non-default versions could not be removed\n", policyName)
			continue
		}

		_, err = iamClient.DeletePolicy(ctx, &iam.DeletePolicyInput{PolicyArn: policy.Arn})
		if err != nil {
			if awsErrCodeIs(err, iamErrCodeNoSuchEntity) {
				fmt.Printf("Policy: Not found (already destroyed) [id=%s]\n", policyName)
			} else {
				fmt.Printf("Failed to delete policy %s: %v\n", policyName, err)
			}
		} else {
			fmt.Printf("Deleted policy: %s\n", policyName)
		}
	}

	return nil
}

func filterIAMRolesWithPrefix(ctx context.Context, iamClient *iam.Client, prefix string) ([]iamtypes.Role, error) {
	var roles []iamtypes.Role
	paginator := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, role := range page.Roles {
			if strings.HasPrefix(aws.ToString(role.RoleName), prefix) {
				roles = append(roles, role)
			}
		}
	}
	return roles, nil
}

func detachRolePolicies(ctx context.Context, iamClient *iam.Client, roleName *string) error {
	if roleName == nil {
		return fmt.Errorf("roleName is nil")
	}
	paginator := iam.NewListAttachedRolePoliciesPaginator(iamClient, &iam.ListAttachedRolePoliciesInput{
		RoleName: roleName,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, policy := range page.AttachedPolicies {
			if policy.PolicyArn == nil {
				fmt.Printf("Warning: PolicyArn is nil for a policy attached to: %s\n", aws.ToString(roleName))
				continue
			}
			_, err := iamClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
				RoleName:  roleName,
				PolicyArn: policy.PolicyArn,
			})
			if err != nil {
				fmt.Printf("Failed to detach policy %s from role %s: %v\n", aws.ToString(policy.PolicyArn), aws.ToString(roleName), err)
			}
		}
	}
	return nil
}

func removeRoleFromInstanceProfiles(ctx context.Context, iamClient *iam.Client, roleName *string) error {
	if roleName == nil {
		return fmt.Errorf("roleName is nil")
	}
	var removeErr error
	paginator := iam.NewListInstanceProfilesForRolePaginator(iamClient, &iam.ListInstanceProfilesForRoleInput{
		RoleName: roleName,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, profile := range page.InstanceProfiles {
			_, err := iamClient.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
				InstanceProfileName: profile.InstanceProfileName,
				RoleName:            roleName,
			})
			if err != nil {
				if awsErrCodeIs(err, iamErrCodeNoSuchEntity) {
					fmt.Printf("Instance profile/role link not found (already removed) [role=%s,profile=%s]\n", aws.ToString(roleName), aws.ToString(profile.InstanceProfileName))
					continue
				}
				fmt.Printf("Failed to remove role %s from instance profile %s: %v\n", aws.ToString(roleName), aws.ToString(profile.InstanceProfileName), err)
				removeErr = errors.Join(removeErr, err)
			} else {
				fmt.Printf("Removed role %s from instance profile %s\n", aws.ToString(roleName), aws.ToString(profile.InstanceProfileName))
			}
		}
	}
	if removeErr != nil {
		return fmt.Errorf("failed to remove role %s from one or more instance profiles: %w", aws.ToString(roleName), removeErr)
	}
	return nil
}

// awsErrCodeIs reports whether err is an AWS API error with the given code.
// SDK v2 errors implement the smithy.APIError interface.
func awsErrCodeIs(err error, code string) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == code
}

func cleanupPersistentVolumes(ctx context.Context, ec2Client *ec2.Client) error {
	var toDeleteVolumes []ec2types.Volume
	paginator := ec2.NewDescribeVolumesPaginator(ec2Client, &ec2.DescribeVolumesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:Name"), Values: []string{"consul-k8s-*"}},
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			fmt.Println("Failed DescribeVolumesPaginator.NextPage.")
			return err
		}
		toDeleteVolumes = append(toDeleteVolumes, page.Volumes...)
	}

	if len(toDeleteVolumes) == 0 {
		fmt.Println("No test volumes found to clean up.")
		return nil
	}

	for _, volume := range toDeleteVolumes {
		volumeID := aws.ToString(volume.VolumeId)
		if volume.State == ec2types.VolumeStateAvailable {
			fmt.Printf("Deleting volume %s\n", volumeID)
			_, err := ec2Client.DeleteVolume(ctx, &ec2.DeleteVolumeInput{VolumeId: volume.VolumeId})
			if err != nil {
				if awsErrCodeIs(err, ec2ErrCodeInvalidVolumeNotFound) {
					fmt.Printf("Volume: Not found (already destroyed) [id=%s]\n", volumeID)
				} else {
					fmt.Printf("Failed to delete volume %s: %s\n", volumeID, err)
				}
			} else {
				fmt.Printf("Successfully deleted volume %s\n", volumeID)
			}
		} else {
			fmt.Printf("Volume %s is not in 'available' state, skipping deletion\n", volumeID)
		}
	}

	return nil
}
