package aws_steampipe

import (
	"fmt"
	"strconv"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
	"diagram/internal/steampipe"
)

// tables lists every table the reader serves, in the order the model adds
// them.
var tables = []table{
	{
		Table: steampipe.Table{
			Name: "aws_ec2_instance", IDColumn: "instance_id",
			Columns: []string{
				"instance_id", "title", "instance_state", "instance_type", "platform_details",
				"private_ip_address", "public_ip_address", "subnet_id", "vpc_id", "security_groups",
			},
		},
		typ:    aws.TypeInstance,
		record: instanceRecord,
	},
	{
		Table: steampipe.Table{
			Name: "aws_vpc", IDColumn: "vpc_id", Columns: []string{"vpc_id", "title", "cidr_block"},
		},
		typ:    aws.TypeVPC,
		record: vpcRecord,
	},
	{
		Table: steampipe.Table{
			Name:     "aws_vpc_subnet",
			IDColumn: "subnet_id",
			Columns: []string{
				"subnet_id",
				"title",
				"vpc_id",
				"cidr_block",
				"availability_zone",
				"map_public_ip_on_launch",
			},
		},
		typ:    aws.TypeSubnet,
		record: subnetRecord,
	},
	{
		Table: steampipe.Table{
			Name: "aws_vpc_security_group", IDColumn: "group_id",
			Columns: []string{"group_id", "group_name", "vpc_id", "ip_permissions", "ip_permissions_egress"},
		},
		typ:    aws.TypeSecurityGroup,
		record: securityGroupRecord,
	},
}

// text returns a column's value, or nil for NULL or an empty value, as the
// SDK leaves an absent field.
func text(row steampipe.Row, column string) *string {
	if v := steampipe.Text(row, column); v != "" {
		return &v
	}
	return nil
}

// ip reads an IP address column. Steampipe can store these as inet, which
// reads as "10.0.0.1/32".
func ip(row steampipe.Row, column string) *string {
	v, _, _ := strings.Cut(steampipe.Text(row, column), "/")
	if v == "" {
		return nil
	}
	return &v
}

// instanceRecord reads an aws_ec2_instance row. Security groups are stored as
// the SDK's GroupIdentifier JSON.
func instanceRecord(row steampipe.Row) (inventory.Record, bool, error) {
	inst := ec2types.Instance{
		InstanceId: text(row, "instance_id"),
		Tags:       aws.NameTags(steampipe.Text(row, "title")),
		State: &ec2types.InstanceState{
			Name: ec2types.InstanceStateName(steampipe.Text(row, "instance_state")),
		},
		InstanceType:     ec2types.InstanceType(steampipe.Text(row, "instance_type")),
		PlatformDetails:  text(row, "platform_details"),
		PrivateIpAddress: ip(row, "private_ip_address"),
		PublicIpAddress:  ip(row, "public_ip_address"),
		SubnetId:         text(row, "subnet_id"),
		VpcId:            text(row, "vpc_id"),
	}
	if err := steampipe.Decode(row, "security_groups", &inst.SecurityGroups); err != nil {
		return inventory.Record{}, false, fmt.Errorf("instance %s: %w", awssdk.ToString(inst.InstanceId), err)
	}
	rec, ok := aws.Instance(inst)
	return rec, ok, nil
}

// vpcRecord reads an aws_vpc row.
func vpcRecord(row steampipe.Row) (inventory.Record, bool, error) {
	return aws.VPC(ec2types.Vpc{
		VpcId:     text(row, "vpc_id"),
		Tags:      aws.NameTags(steampipe.Text(row, "title")),
		CidrBlock: text(row, "cidr_block"),
	}), true, nil
}

// subnetRecord reads an aws_vpc_subnet row.
func subnetRecord(row steampipe.Row) (inventory.Record, bool, error) {
	public, _ := strconv.ParseBool(steampipe.Text(row, "map_public_ip_on_launch"))
	return aws.Subnet(ec2types.Subnet{
		SubnetId:            text(row, "subnet_id"),
		Tags:                aws.NameTags(steampipe.Text(row, "title")),
		VpcId:               text(row, "vpc_id"),
		CidrBlock:           text(row, "cidr_block"),
		AvailabilityZone:    text(row, "availability_zone"),
		MapPublicIpOnLaunch: &public,
	}), true, nil
}

// securityGroupRecord reads an aws_vpc_security_group row. Its permissions
// are stored as the SDK's IpPermission JSON.
func securityGroupRecord(row steampipe.Row) (inventory.Record, bool, error) {
	sg := ec2types.SecurityGroup{
		GroupId:   text(row, "group_id"),
		GroupName: text(row, "group_name"),
		VpcId:     text(row, "vpc_id"),
	}
	id := awssdk.ToString(sg.GroupId)
	if sg.GroupName == nil {
		sg.GroupName = sg.GroupId
	}
	if err := steampipe.Decode(row, "ip_permissions", &sg.IpPermissions); err != nil {
		return inventory.Record{}, false, fmt.Errorf("security group %s: %w", id, err)
	}
	if err := steampipe.Decode(row, "ip_permissions_egress", &sg.IpPermissionsEgress); err != nil {
		return inventory.Record{}, false, fmt.Errorf("security group %s: %w", id, err)
	}
	return aws.SecurityGroup(sg), true, nil
}
