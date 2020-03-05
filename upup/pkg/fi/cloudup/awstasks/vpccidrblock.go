/*
Copyright 2019 The Kubernetes Authors.

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

package awstasks

import (
	"fmt"

	"github.com/aws/aws-sdk-go/service/ec2"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/cloudup/awsup"
	"k8s.io/kops/upup/pkg/fi/cloudup/cloudformation"
	"k8s.io/kops/upup/pkg/fi/cloudup/terraform"
)

//go:generate fitask -type=VPCCIDRBlock
type VPCCIDRBlock struct {
	Name      *string
	Lifecycle *fi.Lifecycle

	VPC        *VPC
	CIDRBlocks *[]string

	// Shared is set if this is a shared VPC
	Shared *bool
}

func (e *VPCCIDRBlock) Find(c *fi.Context) (*VPCCIDRBlock, error) {
	cloud := c.Cloud.(awsup.AWSCloud)

	vpcID := e.VPC.ID

	request := &ec2.DescribeVpcsInput{}

	if fi.StringValue(vpcID) != "" {
		request.VpcIds = []*string{vpcID}
	} else {
		request.Filters = cloud.BuildFilters(e.Name)
	}

	response, err := cloud.EC2().DescribeVpcs(request)
	if err != nil {
		return nil, fmt.Errorf("error listing VPCs: %v", err)
	}
	if response == nil || len(response.Vpcs) == 0 {
		return nil, nil
	}

	if vpcID == nil {
		return nil, nil
	}

	fmt.Printf("SPLAINDEBUG: %v", response)

	cidrBlockAssociationSets := response.Vpcs[0].CidrBlockAssociationSet
	var associatedcidrBlocks []string

	for _, cidrBlockAssociationSet := range cidrBlockAssociationSets {
		state := cidrBlockAssociationSet.CidrBlockState.State
		cidrblock := cidrBlockAssociationSet.CidrBlock

		if *state == "associated" {
			associatedcidrBlocks = append(associatedcidrBlocks, *cidrblock)
			break
		}
	}

	actual := &VPCCIDRBlock{}
	actual.CIDRBlocks = &associatedcidrBlocks
	actual.VPC = &VPC{ID: response.Vpcs[0].VpcId}

	// Prevent spurious changes
	actual.Shared = e.Shared
	actual.Name = e.Name
	actual.Lifecycle = e.Lifecycle

	return actual, nil
}

func (e *VPCCIDRBlock) Run(c *fi.Context) error {
	return fi.DefaultDeltaRunMethod(e, c)
}

func (s *VPCCIDRBlock) CheckChanges(a, e, changes *VPCCIDRBlock) error {
	if e.VPC == nil {
		return fi.RequiredField("VPC")
	}

	if a != nil && changes != nil {
		if changes.VPC != nil {
			return fi.CannotChangeField("VPC")
		}
	}

	return nil
}

func (_ *VPCCIDRBlock) RenderAWS(t *awsup.AWSAPITarget, a, e, changes *VPCCIDRBlock) error {
	shared := fi.BoolValue(e.Shared)
	if shared {
		// Verify the CIDR block was found.
		if a == nil {
			return fmt.Errorf("CIDR block %q not found", fi.StringValue(e.CIDRBlocks))
		}
	}

	if changes.CIDRBlock != nil {
		request := &ec2.AssociateVpcCidrBlockInput{
			VpcId:     e.VPC.ID,
			CidrBlock: e.CIDRBlocks,
		}

		_, err := t.Cloud.EC2().AssociateVpcCidrBlock(request)
		if err != nil {
			return fmt.Errorf("error associating AdditionalCIDR to VPC: %v", err)
		}
	}

	return nil // no tags
}

type terraformVPCCIDRBlock struct {
	VPCID     *terraform.Literal `json:"vpc_id"`
	CIDRBlock *string            `json:"cidr_block"`
}

func (_ *VPCCIDRBlock) RenderTerraform(t *terraform.TerraformTarget, a, e, changes *VPCCIDRBlock) error {

	// When this has been enabled please fix test TestAdditionalCIDR in integration_test.go to run runTestAWS.
	tf := &terraformVPCCIDRBlock{
		VPCID:     e.VPC.TerraformLink(),
		CIDRBlock: e.CIDRBlocks,
	}

	// Terraform 0.12 doesn't support resource names that start with digits. See #7052
	// and https://www.terraform.io/upgrade-guides/0-12.html#pre-upgrade-checklist
	name := fmt.Sprintf("cidr-%v", *e.Name)
	return t.RenderResource("aws_vpc_ipv4_cidr_block_association", name, tf)
}

type cloudformationVPCCIDRBlock struct {
	VPCID     *cloudformation.Literal `json:"VpcId"`
	CIDRBlock *string                 `json:"CidrBlock"`
}

func (_ *VPCCIDRBlock) RenderCloudformation(t *cloudformation.CloudformationTarget, a, e, changes *VPCCIDRBlock) error {
	cf := &cloudformationVPCCIDRBlock{
		VPCID:     e.VPC.CloudformationLink(),
		CIDRBlock: e.CIDRBlocks,
	}

	return t.RenderResource("AWS::EC2::VPCCidrBlock", *e.Name, cf)
}
