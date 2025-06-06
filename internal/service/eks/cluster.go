// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package eks

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/customdiff"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/enum"
	"github.com/hashicorp/terraform-provider-aws/internal/errs"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	"github.com/hashicorp/terraform-provider-aws/internal/flex"
	tfslices "github.com/hashicorp/terraform-provider-aws/internal/slices"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
	"github.com/hashicorp/terraform-provider-aws/names"
)

// @SDKResource("aws_eks_cluster", name="Cluster")
// @Tags(identifierAttribute="arn")
func resourceCluster() *schema.Resource {
	return &schema.Resource{
		CreateWithoutTimeout: resourceClusterCreate,
		ReadWithoutTimeout:   resourceClusterRead,
		UpdateWithoutTimeout: resourceClusterUpdate,
		DeleteWithoutTimeout: resourceClusterDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		SchemaVersion: 1,
		StateUpgraders: []schema.StateUpgrader{
			{
				Type:    resourceClusterV0().CoreConfigSchema().ImpliedType(),
				Upgrade: clusterStateUpgradeV0,
				Version: 0,
			},
		},

		CustomizeDiff: customdiff.Sequence(
			validateAutoModeCustomizeDiff,
			customdiff.ForceNewIfChange("encryption_config", func(_ context.Context, old, new, meta any) bool {
				// You cannot disable envelope encryption after enabling it. This action is irreversible.
				return len(old.([]any)) == 1 && len(new.([]any)) == 0
			}),
			func(ctx context.Context, rd *schema.ResourceDiff, meta any) error {
				if rd.Id() == "" {
					return nil
				}
				oldValue, newValue := rd.GetChange("compute_config")

				oldComputeConfig := expandComputeConfigRequest(oldValue.([]any))
				newComputeConfig := expandComputeConfigRequest(newValue.([]any))

				if newComputeConfig == nil || oldComputeConfig == nil {
					return nil
				}

				oldRoleARN := aws.ToString(oldComputeConfig.NodeRoleArn)
				newRoleARN := aws.ToString(newComputeConfig.NodeRoleArn)

				// only force new if an existing role has changed, not if a new role is added
				if oldRoleARN != "" && oldRoleARN != newRoleARN {
					if err := rd.ForceNew("compute_config.0.node_role_arn"); err != nil {
						return err
					}
				}
				return nil
			},
		),

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(30 * time.Minute),
			Update: schema.DefaultTimeout(60 * time.Minute),
			Delete: schema.DefaultTimeout(15 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"access_config": {
				Type:     schema.TypeList,
				MaxItems: 1,
				Optional: true,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"authentication_mode": {
							Type:             schema.TypeString,
							Optional:         true,
							Computed:         true,
							ValidateDiagFunc: enum.Validate[types.AuthenticationMode](),
						},
						"bootstrap_cluster_creator_admin_permissions": {
							Type:     schema.TypeBool,
							Optional: true,
							ForceNew: true,
						},
					},
				},
			},
			names.AttrARN: {
				Type:     schema.TypeString,
				Computed: true,
			},
			"bootstrap_self_managed_addons": {
				Type:     schema.TypeBool,
				Optional: true,
				ForceNew: true,
				Default:  true,
			},
			"certificate_authority": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"data": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
			},
			"cluster_id": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"compute_config": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						names.AttrEnabled: {
							Type:     schema.TypeBool,
							Optional: true,
						},
						"node_pools": {
							Type:     schema.TypeSet,
							Optional: true,
							Elem: &schema.Schema{
								Type:         schema.TypeString,
								ValidateFunc: validation.StringInSlice(nodePoolType_Values(), false),
							},
						},
						"node_role_arn": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: verify.ValidARN,
						},
					},
				},
			},
			names.AttrCreatedAt: {
				Type:     schema.TypeString,
				Computed: true,
			},
			"enabled_cluster_log_types": {
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Schema{
					Type:             schema.TypeString,
					ValidateDiagFunc: enum.Validate[types.LogType](),
				},
			},
			"encryption_config": {
				Type:          schema.TypeList,
				MaxItems:      1,
				Optional:      true,
				ConflictsWith: []string{"outpost_config"},
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"provider": {
							Type:     schema.TypeList,
							MaxItems: 1,
							Required: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"key_arn": {
										Type:     schema.TypeString,
										Required: true,
									},
								},
							},
						},
						names.AttrResources: {
							Type:     schema.TypeSet,
							Required: true,
							Elem: &schema.Schema{
								Type:         schema.TypeString,
								ValidateFunc: validation.StringInSlice(resources_Values(), false),
							},
						},
					},
				},
			},
			names.AttrEndpoint: {
				Type:     schema.TypeString,
				Computed: true,
			},
			"force_update_version": {
				Type:     schema.TypeBool,
				Optional: true,
			},
			"identity": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"oidc": {
							Type:     schema.TypeList,
							Computed: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									names.AttrIssuer: {
										Type:     schema.TypeString,
										Computed: true,
									},
								},
							},
						},
					},
				},
			},
			"kubernetes_network_config": {
				Type:          schema.TypeList,
				Optional:      true,
				Computed:      true,
				MaxItems:      1,
				ConflictsWith: []string{"outpost_config"},
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"elastic_load_balancing": {
							Type:     schema.TypeList,
							Optional: true,
							Computed: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									names.AttrEnabled: {
										Type:     schema.TypeBool,
										Optional: true,
									},
								},
							},
						},
						"ip_family": {
							Type:             schema.TypeString,
							Optional:         true,
							Computed:         true,
							ForceNew:         true,
							ValidateDiagFunc: enum.Validate[types.IpFamily](),
						},
						"service_ipv4_cidr": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							ForceNew: true,
							ValidateFunc: validation.All(
								validation.IsCIDRNetwork(12, 24),
								validateIPv4CIDRPrivateRange,
							),
						},
						"service_ipv6_cidr": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
			},
			names.AttrName: {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validClusterName,
			},
			"outpost_config": {
				Type:          schema.TypeList,
				MaxItems:      1,
				Optional:      true,
				ConflictsWith: []string{"encryption_config", "kubernetes_network_config", "remote_network_config"},
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"control_plane_instance_type": {
							Type:     schema.TypeString,
							Required: true,
							ForceNew: true,
						},
						"control_plane_placement": {
							Type:     schema.TypeList,
							MaxItems: 1,
							Optional: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									names.AttrGroupName: {
										Type:     schema.TypeString,
										Required: true,
										ForceNew: true,
									},
								},
							},
						},
						"outpost_arns": {
							Type:     schema.TypeSet,
							Required: true,
							MinItems: 1,
							Elem: &schema.Schema{
								Type: schema.TypeString,
							},
						},
					},
				},
			},
			"platform_version": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"remote_network_config": {
				Type:          schema.TypeList,
				Optional:      true,
				ForceNew:      true,
				MaxItems:      1,
				ConflictsWith: []string{"outpost_config"},
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"remote_node_networks": {
							Type:     schema.TypeList,
							MinItems: 1,
							MaxItems: 1,
							Required: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"cidrs": {
										Type:     schema.TypeSet,
										Optional: true,
										ForceNew: true,
										MinItems: 1,
										Elem: &schema.Schema{
											Type: schema.TypeString,
											ValidateFunc: validation.All(
												verify.ValidIPv4CIDRNetworkAddress,
												validateIPv4CIDRPrivateRange,
											),
										},
									},
								},
							},
						},
						"remote_pod_networks": {
							Type:     schema.TypeList,
							Optional: true,
							Computed: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"cidrs": {
										Type:     schema.TypeSet,
										Optional: true,
										ForceNew: true,
										MinItems: 1,
										Elem: &schema.Schema{
											Type: schema.TypeString,
											ValidateFunc: validation.All(
												verify.ValidIPv4CIDRNetworkAddress,
												validateIPv4CIDRPrivateRange,
											),
										},
									},
								},
							},
						},
					},
				},
			},
			names.AttrRoleARN: {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: verify.ValidARN,
			},
			names.AttrStatus: {
				Type:     schema.TypeString,
				Computed: true,
			},
			"storage_config": {
				Type:     schema.TypeList,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"block_storage": {
							Type:     schema.TypeList,
							Optional: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									names.AttrEnabled: {
										Type:     schema.TypeBool,
										Optional: true,
									},
								},
							},
						},
					},
				},
			},
			names.AttrTags:    tftags.TagsSchema(),
			names.AttrTagsAll: tftags.TagsSchemaComputed(),
			"upgrade_policy": {
				Type:     schema.TypeList,
				MaxItems: 1,
				Optional: true,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"support_type": {
							Type:             schema.TypeString,
							Optional:         true,
							Computed:         true,
							ValidateDiagFunc: enum.Validate[types.SupportType](),
						},
					},
				},
			},
			names.AttrVersion: {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			names.AttrVPCConfig: {
				Type:     schema.TypeList,
				MinItems: 1,
				MaxItems: 1,
				Required: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"cluster_security_group_id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"endpoint_private_access": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  false,
						},
						"endpoint_public_access": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  true,
						},
						"public_access_cidrs": {
							Type:     schema.TypeSet,
							Optional: true,
							Computed: true,
							Elem: &schema.Schema{
								Type:         schema.TypeString,
								ValidateFunc: verify.ValidCIDRNetworkAddress,
							},
						},
						names.AttrSecurityGroupIDs: {
							Type:     schema.TypeSet,
							Optional: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
						names.AttrSubnetIDs: {
							Type:     schema.TypeSet,
							Required: true,
							MinItems: 1,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
						names.AttrVPCID: {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
			},
			"zonal_shift_config": {
				Type:     schema.TypeList,
				MaxItems: 1,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						names.AttrEnabled: {
							Type:     schema.TypeBool,
							Optional: true,
						},
					},
				},
			},
		},
	}
}

func resourceClusterCreate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EKSClient(ctx)

	name := d.Get(names.AttrName).(string)
	input := &eks.CreateClusterInput{
		BootstrapSelfManagedAddons: aws.Bool(d.Get("bootstrap_self_managed_addons").(bool)),
		EncryptionConfig:           expandEncryptionConfig(d.Get("encryption_config").([]any)),
		Logging:                    expandLogging(d.Get("enabled_cluster_log_types").(*schema.Set)),
		Name:                       aws.String(name),
		ResourcesVpcConfig:         expandVpcConfigRequest(d.Get(names.AttrVPCConfig).([]any)),
		RoleArn:                    aws.String(d.Get(names.AttrRoleARN).(string)),
		Tags:                       getTagsIn(ctx),
	}

	if v, ok := d.GetOk("compute_config"); ok {
		input.ComputeConfig = expandComputeConfigRequest(v.([]any))
	}

	if v, ok := d.GetOk("access_config"); ok {
		input.AccessConfig = expandCreateAccessConfigRequest(v.([]any))
	}

	if v, ok := d.GetOk("kubernetes_network_config"); ok {
		input.KubernetesNetworkConfig = expandKubernetesNetworkConfigRequest(v.([]any))
	}

	if v, ok := d.GetOk("outpost_config"); ok {
		input.OutpostConfig = expandOutpostConfigRequest(v.([]any))
	}

	if v, ok := d.GetOk("remote_network_config"); ok {
		input.RemoteNetworkConfig = expandRemoteNetworkConfigRequest(v.([]any))
	}

	if v, ok := d.GetOk("storage_config"); ok {
		input.StorageConfig = expandStorageConfigRequest(v.([]any))
	}

	if v, ok := d.GetOk("upgrade_policy"); ok {
		input.UpgradePolicy = expandUpgradePolicy(v.([]any))
	}

	if v, ok := d.GetOk(names.AttrVersion); ok {
		input.Version = aws.String(v.(string))
	}

	if v, ok := d.GetOk("zonal_shift_config"); ok {
		input.ZonalShiftConfig = expandZonalShiftConfig(v.([]any))
	}

	outputRaw, err := tfresource.RetryWhen(ctx, propagationTimeout,
		func() (any, error) {
			return conn.CreateCluster(ctx, input)
		},
		func(err error) (bool, error) {
			// InvalidParameterException: roleArn, arn:aws:iam::123456789012:role/XXX, does not exist
			if errs.IsAErrorMessageContains[*types.InvalidParameterException](err, "does not exist") {
				return true, err
			}

			// InvalidParameterException: Error in role params
			if errs.IsAErrorMessageContains[*types.InvalidParameterException](err, "Error in role params") {
				return true, err
			}

			if errs.IsAErrorMessageContains[*types.InvalidParameterException](err, "Role could not be assumed because the trusted entity is not correct") {
				return true, err
			}

			// InvalidParameterException: The provided role doesn't have the Amazon EKS Managed Policies associated with it. Please ensure the following policy is attached: arn:aws:iam::aws:policy/AmazonEKSClusterPolicy
			if errs.IsAErrorMessageContains[*types.InvalidParameterException](err, "The provided role doesn't have the Amazon EKS Managed Policies associated with it") {
				return true, err
			}

			// InvalidParameterException: IAM role's policy must include the `ec2:DescribeSubnets` action
			if errs.IsAErrorMessageContains[*types.InvalidParameterException](err, "IAM role's policy must include") {
				return true, err
			}

			return false, err
		},
	)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "creating EKS Cluster (%s): %s", name, err)
	}

	d.SetId(aws.ToString(outputRaw.(*eks.CreateClusterOutput).Cluster.Name))

	if _, err := waitClusterCreated(ctx, conn, d.Id(), d.Timeout(schema.TimeoutCreate)); err != nil {
		return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) create: %s", d.Id(), err)
	}

	return append(diags, resourceClusterRead(ctx, d, meta)...)
}

func resourceClusterRead(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).EKSClient(ctx)

	cluster, err := findClusterByName(ctx, conn, d.Id())

	if !d.IsNewResource() && tfresource.NotFound(err) {
		log.Printf("[WARN] EKS Cluster (%s) not found, removing from state", d.Id())
		d.SetId("")
		return diags
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading EKS Cluster (%s): %s", d.Id(), err)
	}

	// bootstrap_cluster_creator_admin_permissions isn't returned from the AWS API.
	// See https://github.com/aws/containers-roadmap/issues/185#issuecomment-1863025784.
	var bootstrapClusterCreatorAdminPermissions *bool
	if v, ok := d.GetOk("access_config"); ok {
		if apiObject := expandCreateAccessConfigRequest(v.([]any)); apiObject != nil {
			bootstrapClusterCreatorAdminPermissions = apiObject.BootstrapClusterCreatorAdminPermissions
		}
	}
	if err := d.Set("access_config", flattenAccessConfigResponse(cluster.AccessConfig, bootstrapClusterCreatorAdminPermissions)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting access_config: %s", err)
	}
	d.Set(names.AttrARN, cluster.Arn)
	d.Set("bootstrap_self_managed_addons", d.Get("bootstrap_self_managed_addons"))
	if err := d.Set("certificate_authority", flattenCertificate(cluster.CertificateAuthority)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting certificate_authority: %s", err)
	}
	// cluster_id is only relevant for clusters on Outposts.
	if cluster.OutpostConfig != nil {
		d.Set("cluster_id", cluster.Id)
	}
	if err := d.Set("compute_config", flattenComputeConfigResponse(cluster.ComputeConfig)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting compute_config: %s", err)
	}
	d.Set(names.AttrCreatedAt, cluster.CreatedAt.Format(time.RFC3339))
	if err := d.Set("enabled_cluster_log_types", flattenLogging(cluster.Logging)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting enabled_cluster_log_types: %s", err)
	}
	if err := d.Set("encryption_config", flattenEncryptionConfigs(cluster.EncryptionConfig)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting encryption_config: %s", err)
	}
	d.Set(names.AttrEndpoint, cluster.Endpoint)
	if err := d.Set("identity", flattenIdentity(cluster.Identity)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting identity: %s", err)
	}
	if err := d.Set("kubernetes_network_config", flattenKubernetesNetworkConfigResponse(cluster.KubernetesNetworkConfig)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting kubernetes_network_config: %s", err)
	}
	d.Set(names.AttrName, cluster.Name)
	if err := d.Set("outpost_config", flattenOutpostConfigResponse(cluster.OutpostConfig)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting outpost_config: %s", err)
	}
	d.Set("platform_version", cluster.PlatformVersion)
	if err := d.Set("remote_network_config", flattenRemoteNetworkConfigResponse(cluster.RemoteNetworkConfig)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting remote_network_config: %s", err)
	}
	d.Set(names.AttrRoleARN, cluster.RoleArn)
	d.Set(names.AttrStatus, cluster.Status)
	if err := d.Set("storage_config", flattenStorageConfigResponse(cluster.StorageConfig)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting storage_config: %s", err)
	}
	if err := d.Set("upgrade_policy", flattenUpgradePolicy(cluster.UpgradePolicy)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting upgrade_policy: %s", err)
	}
	d.Set(names.AttrVersion, cluster.Version)
	if err := d.Set(names.AttrVPCConfig, flattenVPCConfigResponse(cluster.ResourcesVpcConfig)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting vpc_config: %s", err)
	}
	if err := d.Set("zonal_shift_config", flattenZonalShiftConfig(cluster.ZonalShiftConfig)); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting zonal_shift_config: %s", err)
	}

	setTagsOut(ctx, cluster.Tags)

	return diags
}

func resourceClusterUpdate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EKSClient(ctx)

	// Do any version update first.
	if d.HasChange(names.AttrVersion) {
		input := &eks.UpdateClusterVersionInput{
			Name:    aws.String(d.Id()),
			Version: aws.String(d.Get(names.AttrVersion).(string)),
		}

		if v, ok := d.GetOk("force_update_version"); ok {
			input.Force = v.(bool)
		}

		output, err := conn.UpdateClusterVersion(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating EKS Cluster (%s) version: %s", d.Id(), err)
		}

		updateID := aws.ToString(output.Update.Id)

		if _, err := waitClusterUpdateSuccessful(ctx, conn, d.Id(), updateID, d.Timeout(schema.TimeoutUpdate)); err != nil {
			return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) version update (%s): %s", d.Id(), updateID, err)
		}
	}

	if d.HasChange("access_config") {
		if v, ok := d.GetOk("access_config"); ok {
			input := &eks.UpdateClusterConfigInput{
				AccessConfig: expandUpdateAccessConfigRequest(v.([]any)),
				Name:         aws.String(d.Id()),
			}

			output, err := conn.UpdateClusterConfig(ctx, input)

			if err != nil {
				return sdkdiag.AppendErrorf(diags, "updating EKS Cluster (%s) access configuration: %s", d.Id(), err)
			}

			updateID := aws.ToString(output.Update.Id)

			_, err = waitClusterUpdateSuccessful(ctx, conn, d.Id(), updateID, d.Timeout(schema.TimeoutUpdate))

			if err != nil {
				return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) access configuration update (%s): %s", d.Id(), updateID, err)
			}
		}
	}

	if d.HasChanges("compute_config", "kubernetes_network_config", "storage_config") {
		computeConfig := expandComputeConfigRequest(d.Get("compute_config").([]any))
		kubernetesNetworkConfig := expandKubernetesNetworkConfigRequest(d.Get("kubernetes_network_config").([]any))
		storageConfig := expandStorageConfigRequest(d.Get("storage_config").([]any))

		input := &eks.UpdateClusterConfigInput{
			Name:                    aws.String(d.Id()),
			ComputeConfig:           computeConfig,
			KubernetesNetworkConfig: kubernetesNetworkConfig,
			StorageConfig:           storageConfig,
		}

		output, err := conn.UpdateClusterConfig(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating EKS Cluster (%s) compute config: %s", d.Id(), err)
		}

		updateID := aws.ToString(output.Update.Id)

		if _, err = waitClusterUpdateSuccessful(ctx, conn, d.Id(), updateID, d.Timeout(schema.TimeoutUpdate)); err != nil {
			return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) compute config update (%s): %s", d.Id(), updateID, err)
		}
	}

	if d.HasChange("encryption_config") {
		o, n := d.GetChange("encryption_config")

		if len(o.([]any)) == 0 && len(n.([]any)) == 1 {
			input := &eks.AssociateEncryptionConfigInput{
				ClusterName:      aws.String(d.Id()),
				EncryptionConfig: expandEncryptionConfig(d.Get("encryption_config").([]any)),
			}

			output, err := conn.AssociateEncryptionConfig(ctx, input)

			if err != nil {
				return sdkdiag.AppendErrorf(diags, "associating EKS Cluster (%s) encryption config: %s", d.Id(), err)
			}

			updateID := aws.ToString(output.Update.Id)

			if _, err := waitClusterUpdateSuccessful(ctx, conn, d.Id(), updateID, d.Timeout(schema.TimeoutUpdate)); err != nil {
				return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) encryption config association (%s): %s", d.Id(), updateID, err)
			}
		}
	}

	if d.HasChange("enabled_cluster_log_types") {
		input := &eks.UpdateClusterConfigInput{
			Logging: expandLogging(d.Get("enabled_cluster_log_types").(*schema.Set)),
			Name:    aws.String(d.Id()),
		}

		output, err := conn.UpdateClusterConfig(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating EKS Cluster (%s) logging: %s", d.Id(), err)
		}

		updateID := aws.ToString(output.Update.Id)

		if _, err := waitClusterUpdateSuccessful(ctx, conn, d.Id(), updateID, d.Timeout(schema.TimeoutUpdate)); err != nil {
			return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) logging update (%s): %s", d.Id(), updateID, err)
		}
	}

	if d.HasChange("upgrade_policy") {
		input := &eks.UpdateClusterConfigInput{
			Name:          aws.String(d.Id()),
			UpgradePolicy: expandUpgradePolicy(d.Get("upgrade_policy").([]any)),
		}

		output, err := conn.UpdateClusterConfig(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating EKS Cluster (%s) upgrade policy: %s", d.Id(), err)
		}

		updateID := aws.ToString(output.Update.Id)

		if _, err := waitClusterUpdateSuccessful(ctx, conn, d.Id(), updateID, d.Timeout(schema.TimeoutUpdate)); err != nil {
			return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) upgrade policy update (%s): %s", d.Id(), updateID, err)
		}
	}

	if d.HasChanges("vpc_config.0.endpoint_private_access", "vpc_config.0.endpoint_public_access", "vpc_config.0.public_access_cidrs") {
		config := &types.VpcConfigRequest{
			EndpointPrivateAccess: aws.Bool(d.Get("vpc_config.0.endpoint_private_access").(bool)),
			EndpointPublicAccess:  aws.Bool(d.Get("vpc_config.0.endpoint_public_access").(bool)),
		}

		if v, ok := d.GetOk("vpc_config.0.public_access_cidrs"); ok && v.(*schema.Set).Len() > 0 {
			config.PublicAccessCidrs = flex.ExpandStringValueSet(v.(*schema.Set))
		}

		if err := updateVPCConfig(ctx, conn, d.Id(), config, d.Timeout(schema.TimeoutUpdate)); err != nil {
			return sdkdiag.AppendFromErr(diags, err)
		}
	}

	// API only allows one type of update at at time.
	if d.HasChange("vpc_config.0.subnet_ids") {
		config := &types.VpcConfigRequest{
			SubnetIds: flex.ExpandStringValueSet(d.Get("vpc_config.0.subnet_ids").(*schema.Set)),
		}

		if err := updateVPCConfig(ctx, conn, d.Id(), config, d.Timeout(schema.TimeoutUpdate)); err != nil {
			return sdkdiag.AppendFromErr(diags, err)
		}
	}

	if d.HasChange("vpc_config.0.security_group_ids") {
		config := &types.VpcConfigRequest{
			SecurityGroupIds: flex.ExpandStringValueSet(d.Get("vpc_config.0.security_group_ids").(*schema.Set)),
		}

		if err := updateVPCConfig(ctx, conn, d.Id(), config, d.Timeout(schema.TimeoutUpdate)); err != nil {
			return sdkdiag.AppendFromErr(diags, err)
		}
	}

	if d.HasChange("zonal_shift_config") {
		input := &eks.UpdateClusterConfigInput{
			Name:             aws.String(d.Id()),
			ZonalShiftConfig: expandZonalShiftConfig(d.Get("zonal_shift_config").([]any)),
		}

		output, err := conn.UpdateClusterConfig(ctx, input)

		if err != nil {
			return sdkdiag.AppendErrorf(diags, "updating EKS Cluster (%s) zonal shift config: %s", d.Id(), err)
		}

		updateID := aws.ToString(output.Update.Id)

		if _, err := waitClusterUpdateSuccessful(ctx, conn, d.Id(), updateID, d.Timeout(schema.TimeoutUpdate)); err != nil {
			return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) zonal shift config update (%s): %s", d.Id(), updateID, err)
		}
	}

	return append(diags, resourceClusterRead(ctx, d, meta)...)
}

func resourceClusterDelete(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	var diags diag.Diagnostics

	conn := meta.(*conns.AWSClient).EKSClient(ctx)

	input := &eks.DeleteClusterInput{
		Name: aws.String(d.Id()),
	}

	// If a cluster is scaling up due to load a delete request will fail
	// This is a temporary workaround until EKS supports multiple parallel mutating operations
	const (
		timeout = 60 * time.Minute
	)
	log.Printf("[DEBUG] Deleting EKS Cluster: %s", d.Id())
	err := tfresource.Retry(ctx, timeout, func() *retry.RetryError {
		var err error

		_, err = conn.DeleteCluster(ctx, input)

		if errs.IsAErrorMessageContains[*types.ResourceInUseException](err, "in progress") {
			return retry.RetryableError(err)
		}

		if err != nil {
			return retry.NonRetryableError(err)
		}

		return nil
	}, tfresource.WithDelayRand(1*time.Minute), tfresource.WithPollInterval(30*time.Second))

	if tfresource.TimedOut(err) {
		_, err = conn.DeleteCluster(ctx, input)
	}

	if errs.IsA[*types.ResourceNotFoundException](err) {
		return diags
	}

	// Sometimes the EKS API returns the ResourceNotFound error in this form:
	// ClientException: No cluster found for name: tf-acc-test-0o1f8
	if errs.IsAErrorMessageContains[*types.ClientException](err, "No cluster found for name:") {
		return diags
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting EKS Cluster (%s): %s", d.Id(), err)
	}

	if _, err := waitClusterDeleted(ctx, conn, d.Id(), d.Timeout(schema.TimeoutDelete)); err != nil {
		return sdkdiag.AppendErrorf(diags, "waiting for EKS Cluster (%s) delete: %s", d.Id(), err)
	}

	return diags
}

func findClusterByName(ctx context.Context, conn *eks.Client, name string) (*types.Cluster, error) {
	input := &eks.DescribeClusterInput{
		Name: aws.String(name),
	}

	output, err := conn.DescribeCluster(ctx, input)

	// Sometimes the EKS API returns the ResourceNotFound error in this form:
	// ClientException: No cluster found for name: tf-acc-test-0o1f8
	if errs.IsA[*types.ResourceNotFoundException](err) || errs.IsAErrorMessageContains[*types.ClientException](err, "No cluster found for name:") {
		return nil, &retry.NotFoundError{
			LastError:   err,
			LastRequest: input,
		}
	}

	if err != nil {
		return nil, err
	}

	if output == nil || output.Cluster == nil {
		return nil, tfresource.NewEmptyResultError(input)
	}

	return output.Cluster, nil
}

func updateVPCConfig(ctx context.Context, conn *eks.Client, name string, vpcConfig *types.VpcConfigRequest, timeout time.Duration) error {
	input := &eks.UpdateClusterConfigInput{
		Name:               aws.String(name),
		ResourcesVpcConfig: vpcConfig,
	}

	output, err := conn.UpdateClusterConfig(ctx, input)

	if err != nil {
		return fmt.Errorf("updating EKS Cluster (%s) VPC configuration: %s", name, err)
	}

	updateID := aws.ToString(output.Update.Id)

	if _, err := waitClusterUpdateSuccessful(ctx, conn, name, updateID, timeout); err != nil {
		return fmt.Errorf("waiting for EKS Cluster (%s) VPC configuration update (%s): %s", name, updateID, err)
	}

	return nil
}

func findClusterUpdateByTwoPartKey(ctx context.Context, conn *eks.Client, name, id string) (*types.Update, error) {
	input := &eks.DescribeUpdateInput{
		Name:     aws.String(name),
		UpdateId: aws.String(id),
	}

	output, err := conn.DescribeUpdate(ctx, input)

	if errs.IsA[*types.ResourceNotFoundException](err) {
		return nil, &retry.NotFoundError{
			LastError:   err,
			LastRequest: input,
		}
	}

	if err != nil {
		return nil, err
	}

	if output == nil || output.Update == nil {
		return nil, tfresource.NewEmptyResultError(input)
	}

	return output.Update, nil
}

func statusCluster(ctx context.Context, conn *eks.Client, name string) retry.StateRefreshFunc {
	return func() (any, string, error) {
		output, err := findClusterByName(ctx, conn, name)

		if tfresource.NotFound(err) {
			return nil, "", nil
		}

		if err != nil {
			return nil, "", err
		}

		return output, string(output.Status), nil
	}
}

func statusClusterUpdate(ctx context.Context, conn *eks.Client, name, id string) retry.StateRefreshFunc {
	return func() (any, string, error) {
		output, err := findClusterUpdateByTwoPartKey(ctx, conn, name, id)

		if tfresource.NotFound(err) {
			return nil, "", nil
		}

		if err != nil {
			return nil, "", err
		}

		return output, string(output.Status), nil
	}
}

func waitClusterCreated(ctx context.Context, conn *eks.Client, name string, timeout time.Duration) (*types.Cluster, error) {
	stateConf := &retry.StateChangeConf{
		Pending: enum.Slice(types.ClusterStatusPending, types.ClusterStatusCreating),
		Target:  enum.Slice(types.ClusterStatusActive),
		Refresh: statusCluster(ctx, conn, name),
		Timeout: timeout,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*types.Cluster); ok {
		return output, err
	}

	return nil, err
}

func waitClusterDeleted(ctx context.Context, conn *eks.Client, name string, timeout time.Duration) (*types.Cluster, error) {
	stateConf := &retry.StateChangeConf{
		Pending:    enum.Slice(types.ClusterStatusActive, types.ClusterStatusDeleting),
		Target:     []string{},
		Refresh:    statusCluster(ctx, conn, name),
		Timeout:    timeout,
		MinTimeout: 10 * time.Second,
		// An attempt to avoid "ResourceInUseException: Cluster already exists with name: ..." errors
		// in acceptance tests when recreating a cluster with the same randomly generated name.
		ContinuousTargetOccurence: 3,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*types.Cluster); ok {
		return output, err
	}

	return nil, err
}

func waitClusterUpdateSuccessful(ctx context.Context, conn *eks.Client, name, id string, timeout time.Duration) (*types.Update, error) { //nolint:unparam
	stateConf := &retry.StateChangeConf{
		Pending: enum.Slice(types.UpdateStatusInProgress),
		Target:  enum.Slice(types.UpdateStatusSuccessful),
		Refresh: statusClusterUpdate(ctx, conn, name, id),
		Timeout: timeout,
	}

	outputRaw, err := stateConf.WaitForStateContext(ctx)

	if output, ok := outputRaw.(*types.Update); ok {
		if status := output.Status; status == types.UpdateStatusCancelled || status == types.UpdateStatusFailed {
			tfresource.SetLastError(err, errorDetailsError(output.Errors))
		}

		return output, err
	}

	return nil, err
}

func expandCreateAccessConfigRequest(tfList []any) *types.CreateAccessConfigRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.CreateAccessConfigRequest{}

	if v, ok := tfMap["authentication_mode"].(string); ok && v != "" {
		apiObject.AuthenticationMode = types.AuthenticationMode(v)
	}

	if v, ok := tfMap["bootstrap_cluster_creator_admin_permissions"].(bool); ok {
		apiObject.BootstrapClusterCreatorAdminPermissions = aws.Bool(v)
	}

	return apiObject
}

func expandUpdateAccessConfigRequest(tfList []any) *types.UpdateAccessConfigRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.UpdateAccessConfigRequest{}

	if v, ok := tfMap["authentication_mode"].(string); ok && v != "" {
		apiObject.AuthenticationMode = types.AuthenticationMode(v)
	}

	return apiObject
}

func expandComputeConfigRequest(tfList []any) *types.ComputeConfigRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.ComputeConfigRequest{}

	if v, ok := tfMap[names.AttrEnabled].(bool); ok {
		apiObject.Enabled = aws.Bool(v)
	}

	if v, ok := tfMap["node_pools"].(*schema.Set); ok && v.Len() > 0 {
		apiObject.NodePools = flex.ExpandStringValueSet(v)
	}

	if v, ok := tfMap["node_role_arn"].(string); ok && v != "" {
		apiObject.NodeRoleArn = aws.String(v)
	}

	return apiObject
}

func expandEncryptionConfig(tfList []any) []types.EncryptionConfig {
	if len(tfList) == 0 {
		return nil
	}

	var apiObjects []types.EncryptionConfig

	for _, tfMapRaw := range tfList {
		tfMap, ok := tfMapRaw.(map[string]any)
		if !ok {
			continue
		}

		apiObject := types.EncryptionConfig{
			Provider: expandProvider(tfMap["provider"].([]any)),
		}

		if v, ok := tfMap[names.AttrResources].(*schema.Set); ok && v.Len() > 0 {
			apiObject.Resources = flex.ExpandStringValueSet(v)
		}

		apiObjects = append(apiObjects, apiObject)
	}

	return apiObjects
}

func expandProvider(tfList []any) *types.Provider {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.Provider{}

	if v, ok := tfMap["key_arn"].(string); ok && v != "" {
		apiObject.KeyArn = aws.String(v)
	}

	return apiObject
}

func expandStorageConfigRequest(tfList []any) *types.StorageConfigRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.StorageConfigRequest{}

	if v, ok := tfMap["block_storage"].([]any); ok {
		apiObject.BlockStorage = expandBlockStorage(v)
	}

	return apiObject
}

func expandBlockStorage(tfList []any) *types.BlockStorage {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.BlockStorage{}

	if v, ok := tfMap[names.AttrEnabled].(bool); ok {
		apiObject.Enabled = aws.Bool(v)
	}

	return apiObject
}

func expandOutpostConfigRequest(tfList []any) *types.OutpostConfigRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	outpostConfigRequest := &types.OutpostConfigRequest{}

	if v, ok := tfMap["control_plane_instance_type"].(string); ok && v != "" {
		outpostConfigRequest.ControlPlaneInstanceType = aws.String(v)
	}

	if v, ok := tfMap["control_plane_placement"].([]any); ok {
		outpostConfigRequest.ControlPlanePlacement = expandControlPlanePlacementRequest(v)
	}

	if v, ok := tfMap["outpost_arns"].(*schema.Set); ok && v.Len() > 0 {
		outpostConfigRequest.OutpostArns = flex.ExpandStringValueSet(v)
	}

	return outpostConfigRequest
}

func expandControlPlanePlacementRequest(tfList []any) *types.ControlPlanePlacementRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.ControlPlanePlacementRequest{}

	if v, ok := tfMap[names.AttrGroupName].(string); ok && v != "" {
		apiObject.GroupName = aws.String(v)
	}

	return apiObject
}

func expandVpcConfigRequest(tfList []any) *types.VpcConfigRequest { // nosemgrep:ci.caps5-in-func-name
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.VpcConfigRequest{
		EndpointPrivateAccess: aws.Bool(tfMap["endpoint_private_access"].(bool)),
		EndpointPublicAccess:  aws.Bool(tfMap["endpoint_public_access"].(bool)),
		SecurityGroupIds:      flex.ExpandStringValueSet(tfMap[names.AttrSecurityGroupIDs].(*schema.Set)),
		SubnetIds:             flex.ExpandStringValueSet(tfMap[names.AttrSubnetIDs].(*schema.Set)),
	}

	if v, ok := tfMap["public_access_cidrs"].(*schema.Set); ok && v.Len() > 0 {
		apiObject.PublicAccessCidrs = flex.ExpandStringValueSet(v)
	}

	return apiObject
}

func expandKubernetesNetworkConfigRequest(tfList []any) *types.KubernetesNetworkConfigRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.KubernetesNetworkConfigRequest{}

	if v, ok := tfMap["elastic_load_balancing"].([]any); ok {
		apiObject.ElasticLoadBalancing = expandKubernetesNetworkConfigElasticLoadBalancing(v)
	}

	if v, ok := tfMap["ip_family"].(string); ok && v != "" {
		apiObject.IpFamily = types.IpFamily(v)
	}

	if v, ok := tfMap["service_ipv4_cidr"].(string); ok && v != "" {
		apiObject.ServiceIpv4Cidr = aws.String(v)
	}

	return apiObject
}

func expandKubernetesNetworkConfigElasticLoadBalancing(tfList []any) *types.ElasticLoadBalancing {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.ElasticLoadBalancing{}

	if v, ok := tfMap[names.AttrEnabled].(bool); ok {
		apiObject.Enabled = aws.Bool(v)
	}

	return apiObject
}

func expandRemoteNetworkConfigRequest(tfList []any) *types.RemoteNetworkConfigRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	apiObject := &types.RemoteNetworkConfigRequest{
		RemoteNodeNetworks: expandRemoteNodeNetworks(tfMap["remote_node_networks"].([]any)),
	}

	if v, ok := tfMap["remote_pod_networks"].([]any); ok {
		apiObject.RemotePodNetworks = expandRemotePodNetworks(v)
	}

	return apiObject
}

func expandRemoteNodeNetworks(tfList []any) []types.RemoteNodeNetwork {
	if len(tfList) == 0 {
		return nil
	}

	var apiObjects []types.RemoteNodeNetwork

	for _, tfMapRaw := range tfList {
		tfMap, ok := tfMapRaw.(map[string]any)
		if !ok {
			continue
		}

		apiObject := types.RemoteNodeNetwork{
			Cidrs: flex.ExpandStringValueSet(tfMap["cidrs"].(*schema.Set)),
		}

		apiObjects = append(apiObjects, apiObject)
	}

	return apiObjects
}

func expandRemotePodNetworks(tfList []any) []types.RemotePodNetwork {
	if len(tfList) == 0 {
		return nil
	}

	var apiObjects []types.RemotePodNetwork

	for _, tfMapRaw := range tfList {
		tfMap, ok := tfMapRaw.(map[string]any)
		if !ok {
			continue
		}

		apiObject := types.RemotePodNetwork{
			Cidrs: flex.ExpandStringValueSet(tfMap["cidrs"].(*schema.Set)),
		}

		apiObjects = append(apiObjects, apiObject)
	}

	return apiObjects
}

func expandLogging(vEnabledLogTypes *schema.Set) *types.Logging {
	allLogTypes := enum.EnumValues[types.LogType]()
	enabledLogTypes := flex.ExpandStringyValueSet[types.LogType](vEnabledLogTypes)

	return &types.Logging{
		ClusterLogging: []types.LogSetup{
			{
				Enabled: aws.Bool(true),
				Types:   enabledLogTypes,
			},
			{
				Enabled: aws.Bool(false),
				Types:   tfslices.RemoveAll(allLogTypes, enabledLogTypes...),
			},
		},
	}
}

func expandUpgradePolicy(tfList []any) *types.UpgradePolicyRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	upgradePolicyRequest := &types.UpgradePolicyRequest{}

	if v, ok := tfMap["support_type"].(string); ok && v != "" {
		upgradePolicyRequest.SupportType = types.SupportType(v)
	}

	return upgradePolicyRequest
}

func expandZonalShiftConfig(tfList []any) *types.ZonalShiftConfigRequest {
	if len(tfList) == 0 {
		return nil
	}

	tfMap, ok := tfList[0].(map[string]any)
	if !ok {
		return nil
	}

	ZonalShiftConfigRequest := &types.ZonalShiftConfigRequest{}

	if v, ok := tfMap[names.AttrEnabled].(bool); ok {
		ZonalShiftConfigRequest.Enabled = aws.Bool(v)
	}

	return ZonalShiftConfigRequest
}

func flattenCertificate(certificate *types.Certificate) []map[string]any {
	if certificate == nil {
		return []map[string]any{}
	}

	m := map[string]any{
		"data": aws.ToString(certificate.Data),
	}

	return []map[string]any{m}
}

func flattenComputeConfigResponse(apiObject *types.ComputeConfigResponse) []map[string]any {
	if apiObject == nil {
		return []map[string]any{}
	}

	m := map[string]any{
		names.AttrEnabled: aws.ToBool(apiObject.Enabled),
		"node_pools":      flex.FlattenStringValueList(apiObject.NodePools),
		"node_role_arn":   aws.ToString(apiObject.NodeRoleArn),
	}

	return []map[string]any{m}
}

func flattenIdentity(identity *types.Identity) []map[string]any {
	if identity == nil {
		return []map[string]any{}
	}

	m := map[string]any{
		"oidc": flattenOIDC(identity.Oidc),
	}

	return []map[string]any{m}
}

func flattenOIDC(oidc *types.OIDC) []map[string]any {
	if oidc == nil {
		return []map[string]any{}
	}

	m := map[string]any{
		names.AttrIssuer: aws.ToString(oidc.Issuer),
	}

	return []map[string]any{m}
}

func flattenAccessConfigResponse(apiObject *types.AccessConfigResponse, bootstrapClusterCreatorAdminPermissions *bool) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		"authentication_mode": apiObject.AuthenticationMode,
	}

	if bootstrapClusterCreatorAdminPermissions != nil {
		tfMap["bootstrap_cluster_creator_admin_permissions"] = aws.ToBool(bootstrapClusterCreatorAdminPermissions)
	} else {
		// Setting default value to true for backward compatibility.
		tfMap["bootstrap_cluster_creator_admin_permissions"] = true
	}

	return []any{tfMap}
}

func flattenEncryptionConfigs(apiObjects []types.EncryptionConfig) []any {
	if len(apiObjects) == 0 {
		return nil
	}

	var tfList []any

	for _, apiObject := range apiObjects {
		tfMap := map[string]any{
			"provider":          flattenProvider(apiObject.Provider),
			names.AttrResources: apiObject.Resources,
		}

		tfList = append(tfList, tfMap)
	}

	return tfList
}

func flattenProvider(apiObject *types.Provider) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		"key_arn": aws.ToString(apiObject.KeyArn),
	}

	return []any{tfMap}
}

func flattenVPCConfigResponse(vpcConfig *types.VpcConfigResponse) []map[string]any { // nosemgrep:ci.caps5-in-func-name
	if vpcConfig == nil {
		return []map[string]any{}
	}

	m := map[string]any{
		"cluster_security_group_id": aws.ToString(vpcConfig.ClusterSecurityGroupId),
		"endpoint_private_access":   vpcConfig.EndpointPrivateAccess,
		"endpoint_public_access":    vpcConfig.EndpointPublicAccess,
		names.AttrSecurityGroupIDs:  vpcConfig.SecurityGroupIds,
		names.AttrSubnetIDs:         vpcConfig.SubnetIds,
		"public_access_cidrs":       vpcConfig.PublicAccessCidrs,
		names.AttrVPCID:             aws.ToString(vpcConfig.VpcId),
	}

	return []map[string]any{m}
}

func flattenLogging(logging *types.Logging) []string {
	enabledLogTypes := []types.LogType{}

	if logging != nil {
		logSetups := logging.ClusterLogging
		for _, logSetup := range logSetups {
			if !aws.ToBool(logSetup.Enabled) {
				continue
			}

			enabledLogTypes = append(enabledLogTypes, logSetup.Types...)
		}
	}

	return enum.Slice(enabledLogTypes...)
}

func flattenKubernetesNetworkConfigResponse(apiObject *types.KubernetesNetworkConfigResponse) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		"elastic_load_balancing": flattenKubernetesNetworkConfigElasticLoadBalancing(apiObject.ElasticLoadBalancing),
		"service_ipv4_cidr":      aws.ToString(apiObject.ServiceIpv4Cidr),
		"service_ipv6_cidr":      aws.ToString(apiObject.ServiceIpv6Cidr),
		"ip_family":              apiObject.IpFamily,
	}

	return []any{tfMap}
}

func flattenKubernetesNetworkConfigElasticLoadBalancing(apiObjects *types.ElasticLoadBalancing) []any {
	if apiObjects == nil {
		return nil
	}

	tfMap := map[string]any{
		names.AttrEnabled: aws.ToBool(apiObjects.Enabled),
	}

	return []any{tfMap}
}

func flattenOutpostConfigResponse(apiObject *types.OutpostConfigResponse) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		"control_plane_instance_type": aws.ToString(apiObject.ControlPlaneInstanceType),
		"control_plane_placement":     flattenControlPlanePlacementResponse(apiObject.ControlPlanePlacement),
		"outpost_arns":                apiObject.OutpostArns,
	}

	return []any{tfMap}
}

func flattenRemoteNetworkConfigResponse(apiObject *types.RemoteNetworkConfigResponse) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		"remote_node_networks": flattenRemoteNodeNetwork(apiObject.RemoteNodeNetworks),
		"remote_pod_networks":  flattenRemotePodNetwork(apiObject.RemotePodNetworks),
	}

	return []any{tfMap}
}

func flattenRemoteNodeNetwork(apiObjects []types.RemoteNodeNetwork) []any {
	if len(apiObjects) == 0 {
		return nil
	}

	var tfList []any

	for _, apiObject := range apiObjects {
		tfMap := map[string]any{
			"cidrs": flex.FlattenStringValueList(apiObject.Cidrs),
		}

		tfList = append(tfList, tfMap)
	}

	return tfList
}

func flattenRemotePodNetwork(apiObjects []types.RemotePodNetwork) []any {
	if len(apiObjects) == 0 {
		return nil
	}

	var tfList []any

	for _, apiObject := range apiObjects {
		tfMap := map[string]any{
			"cidrs": flex.FlattenStringValueList(apiObject.Cidrs),
		}

		tfList = append(tfList, tfMap)
	}

	return tfList
}

func flattenControlPlanePlacementResponse(apiObject *types.ControlPlanePlacementResponse) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		names.AttrGroupName: aws.ToString(apiObject.GroupName),
	}

	return []any{tfMap}
}

func flattenStorageConfigResponse(apiObject *types.StorageConfigResponse) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		"block_storage": flattenBlockStorage(apiObject.BlockStorage),
	}

	return []any{tfMap}
}

func flattenBlockStorage(apiObject *types.BlockStorage) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		names.AttrEnabled: aws.ToBool(apiObject.Enabled),
	}

	return []any{tfMap}
}

func flattenUpgradePolicy(apiObject *types.UpgradePolicyResponse) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		"support_type": apiObject.SupportType,
	}

	return []any{tfMap}
}

func flattenZonalShiftConfig(apiObject *types.ZonalShiftConfigResponse) []any {
	if apiObject == nil {
		return nil
	}

	tfMap := map[string]any{
		names.AttrEnabled: apiObject.Enabled,
	}

	return []any{tfMap}
}

// InvalidParameterException: For EKS Auto Mode, please ensure that all required configs,
// including computeConfig, kubernetesNetworkConfig, and blockStorage are all either fully enabled or fully disabled.
func validateAutoModeCustomizeDiff(_ context.Context, d *schema.ResourceDiff, _ any) error {
	if d.HasChanges("compute_config", "kubernetes_network_config", "storage_config") {
		computeConfig := expandComputeConfigRequest(d.Get("compute_config").([]any))
		kubernetesNetworkConfig := expandKubernetesNetworkConfigRequest(d.Get("kubernetes_network_config").([]any))
		storageConfig := expandStorageConfigRequest(d.Get("storage_config").([]any))

		computeConfigEnabled := computeConfig != nil && computeConfig.Enabled != nil && aws.ToBool(computeConfig.Enabled)
		kubernetesNetworkConfigEnabled := kubernetesNetworkConfig != nil && kubernetesNetworkConfig.ElasticLoadBalancing != nil && kubernetesNetworkConfig.ElasticLoadBalancing.Enabled != nil && aws.ToBool(kubernetesNetworkConfig.ElasticLoadBalancing.Enabled)
		storageConfigEnabled := storageConfig != nil && storageConfig.BlockStorage != nil && storageConfig.BlockStorage.Enabled != nil && aws.ToBool(storageConfig.BlockStorage.Enabled)

		if computeConfigEnabled != kubernetesNetworkConfigEnabled || computeConfigEnabled != storageConfigEnabled {
			return errors.New("compute_config.enabled, kubernetes_networking_config.elastic_load_balancing.enabled, and storage_config.block_storage.enabled must all be set to either true or false")
		}
	}

	return nil
}
