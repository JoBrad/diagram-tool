// Package aws holds what the tool knows about AWS resources, whichever source
// they were read from: their resource types, how each becomes an inventory
// record, and how they connect, contain one another, and are selected.
//
// Sources live in subpackages (aws_sdk calls the AWS APIs, aws_steampipe
// reads Steampipe tables). Each turns what it reads into SDK types and hands
// them to this package's record builders, so both produce the same records.
// Once a source has read every region, Finish resolves containment and
// applies the config's selection settings, after which the model can draw
// the records without knowing they are AWS resources.
package aws

// Platform is the inventory.Record Platform of every AWS record.
const Platform = "aws"

// Resource types, as CloudFormation names them. They are untyped so
// renderers can use them as their own resource type values.
const (
	TypeInstance        = "AWS::EC2::Instance"
	TypeNATGateway      = "AWS::EC2::NatGateway"
	TypeVPNGateway      = "AWS::EC2::VPNGateway"
	TypeInternetGateway = "AWS::EC2::InternetGateway"
	TypeTransitGateway  = "AWS::EC2::TransitGateway"
	TypeCustomerGateway = "AWS::EC2::CustomerGateway"
	TypePrefixList      = "AWS::EC2::PrefixList"
	TypeVPC             = "AWS::EC2::VPC"
	TypeSubnet          = "AWS::EC2::Subnet"
	TypeSecurityGroup   = "AWS::EC2::SecurityGroup"
	TypeVPNConnection   = "AWS::EC2::VPNConnection"
	TypeKMSKey          = "AWS::KMS::Key"
	TypeSecret          = "AWS::SecretsManager::Secret"
	TypeParameter       = "AWS::SSM::Parameter"
)

// RelVPNConnection is the Relation of a VPN connection record, which runs
// from a VPN or transit gateway to a customer gateway.
const RelVPNConnection = "vpn_connection"

// TargetPrefixList is the rule target type of a security group rule that
// targets a managed prefix list.
const TargetPrefixList = "prefix_list"

// Record attribute keys, read by renderers.
const (
	AttrInstanceType    = "instance_type"
	AttrPlatformDetails = "platform_details"
	AttrPrivateIP       = "private_ip"
	AttrPublicIP        = "public_ip"
	// AttrPublicIPs is a comma-separated list.
	AttrPublicIPs = "public_ips"
	AttrIP        = "ip"
	AttrCIDR      = "cidr"
	AttrZone      = "availability_zone"
	// AttrPublic is "true" for a subnet that assigns public IPs on launch.
	AttrPublic = "public"
)
