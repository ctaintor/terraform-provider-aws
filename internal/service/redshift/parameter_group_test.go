// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package redshift_test

import (
	"context"
	"fmt"
	"testing"

	awstypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-provider-aws/internal/acctest"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	tfredshift "github.com/hashicorp/terraform-provider-aws/internal/service/redshift"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/names"
)

func TestAccRedshiftParameterGroup_basic(t *testing.T) {
	ctx := acctest.Context(t)
	var v awstypes.ClusterParameterGroup
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)
	resourceName := "aws_redshift_parameter_group.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(ctx, t) },
		ErrorCheck:               acctest.ErrorCheck(t, names.RedshiftServiceID),
		ProtoV5ProviderFactories: acctest.ProtoV5ProviderFactories,
		CheckDestroy:             testAccCheckParameterGroupDestroy(ctx),
		Steps: []resource.TestStep{
			{
				Config: testAccParameterGroupConfig_basic(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckParameterGroupExists(ctx, resourceName, &v),
					acctest.CheckResourceAttrRegionalARN(ctx, resourceName, names.AttrARN, "redshift", fmt.Sprintf("parametergroup:%s", rName)),
					resource.TestCheckResourceAttr(resourceName, names.AttrDescription, "Managed by Terraform"),
					resource.TestCheckResourceAttr(resourceName, names.AttrFamily, "redshift-1.0"),
					resource.TestCheckResourceAttr(resourceName, names.AttrName, rName),
					resource.TestCheckResourceAttr(resourceName, "parameter.#", "3"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "parameter.*", map[string]string{
						names.AttrName:  "require_ssl",
						names.AttrValue: acctest.CtTrue,
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "parameter.*", map[string]string{
						names.AttrName:  "query_group",
						names.AttrValue: "example",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "parameter.*", map[string]string{
						names.AttrName:  "enable_user_activity_logging",
						names.AttrValue: acctest.CtTrue,
					}),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsPercent, "0"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccRedshiftParameterGroup_disappears(t *testing.T) {
	ctx := acctest.Context(t)
	var v awstypes.ClusterParameterGroup
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)
	resourceName := "aws_redshift_parameter_group.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(ctx, t) },
		ErrorCheck:               acctest.ErrorCheck(t, names.RedshiftServiceID),
		ProtoV5ProviderFactories: acctest.ProtoV5ProviderFactories,
		CheckDestroy:             testAccCheckParameterGroupDestroy(ctx),
		Steps: []resource.TestStep{
			{
				Config: testAccParameterGroupConfig_basic(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckParameterGroupExists(ctx, resourceName, &v),
					acctest.CheckResourceDisappears(ctx, acctest.Provider, tfredshift.ResourceParameterGroup(), resourceName),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAccRedshiftParameterGroup_update(t *testing.T) {
	ctx := acctest.Context(t)
	var v awstypes.ClusterParameterGroup
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)
	resourceName := "aws_redshift_parameter_group.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(ctx, t) },
		ErrorCheck:               acctest.ErrorCheck(t, names.RedshiftServiceID),
		ProtoV5ProviderFactories: acctest.ProtoV5ProviderFactories,
		CheckDestroy:             testAccCheckParameterGroupDestroy(ctx),
		Steps: []resource.TestStep{
			{
				Config: testAccParameterGroupConfig_noParameters(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckParameterGroupExists(ctx, resourceName, &v),
					acctest.CheckResourceAttrRegionalARN(ctx, resourceName, names.AttrARN, "redshift", fmt.Sprintf("parametergroup:%s", rName)),
					resource.TestCheckResourceAttr(resourceName, names.AttrDescription, "Managed by Terraform"),
					resource.TestCheckResourceAttr(resourceName, names.AttrFamily, "redshift-1.0"),
					resource.TestCheckResourceAttr(resourceName, names.AttrName, rName),
					resource.TestCheckResourceAttr(resourceName, "parameter.#", "0"),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsPercent, "0"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccParameterGroupConfig_basic(rName),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckParameterGroupExists(ctx, resourceName, &v),
					acctest.CheckResourceAttrRegionalARN(ctx, resourceName, names.AttrARN, "redshift", fmt.Sprintf("parametergroup:%s", rName)),
					resource.TestCheckResourceAttr(resourceName, names.AttrDescription, "Managed by Terraform"),
					resource.TestCheckResourceAttr(resourceName, names.AttrFamily, "redshift-1.0"),
					resource.TestCheckResourceAttr(resourceName, names.AttrName, rName),
					resource.TestCheckResourceAttr(resourceName, "parameter.#", "3"),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "parameter.*", map[string]string{
						names.AttrName:  "require_ssl",
						names.AttrValue: acctest.CtTrue,
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "parameter.*", map[string]string{
						names.AttrName:  "query_group",
						names.AttrValue: "example",
					}),
					resource.TestCheckTypeSetElemNestedAttrs(resourceName, "parameter.*", map[string]string{
						names.AttrName:  "enable_user_activity_logging",
						names.AttrValue: acctest.CtTrue,
					}),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsPercent, "0"),
				),
			},
		},
	})
}

func TestAccRedshiftParameterGroup_tags(t *testing.T) {
	ctx := acctest.Context(t)
	var v awstypes.ClusterParameterGroup
	rName := sdkacctest.RandomWithPrefix(acctest.ResourcePrefix)
	resourceName := "aws_redshift_parameter_group.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(ctx, t) },
		ErrorCheck:               acctest.ErrorCheck(t, names.RedshiftServiceID),
		ProtoV5ProviderFactories: acctest.ProtoV5ProviderFactories,
		CheckDestroy:             testAccCheckParameterGroupDestroy(ctx),
		Steps: []resource.TestStep{
			{
				Config: testAccParameterGroupConfig_tags1(rName, acctest.CtKey1, acctest.CtValue1),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckParameterGroupExists(ctx, resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, names.AttrDescription, "Test parameter group for terraform"),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsPercent, "1"),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsKey1, acctest.CtValue1),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccParameterGroupConfig_tags2(rName, acctest.CtKey1, acctest.CtValue1Updated, acctest.CtKey2, acctest.CtValue2),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckParameterGroupExists(ctx, resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsPercent, "2"),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsKey1, acctest.CtValue1Updated),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsKey2, acctest.CtValue2),
				),
			},
			{
				Config: testAccParameterGroupConfig_tags1(rName, acctest.CtKey2, acctest.CtValue2),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckParameterGroupExists(ctx, resourceName, &v),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsPercent, "1"),
					resource.TestCheckResourceAttr(resourceName, acctest.CtTagsKey2, acctest.CtValue2),
				),
			},
		},
	})
}

func testAccCheckParameterGroupDestroy(ctx context.Context) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		conn := acctest.Provider.Meta().(*conns.AWSClient).RedshiftClient(ctx)

		for _, rs := range s.RootModule().Resources {
			if rs.Type != "aws_redshift_parameter_group" {
				continue
			}

			_, err := tfredshift.FindParameterGroupByName(ctx, conn, rs.Primary.ID)

			if tfresource.NotFound(err) {
				continue
			}

			if err != nil {
				return err
			}

			return fmt.Errorf("Redshift Parameter Group %s still exists", rs.Primary.ID)
		}

		return nil
	}
}

func testAccCheckParameterGroupExists(ctx context.Context, n string, v *awstypes.ClusterParameterGroup) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No Redshift Parameter Group ID is set")
		}

		conn := acctest.Provider.Meta().(*conns.AWSClient).RedshiftClient(ctx)

		output, err := tfredshift.FindParameterGroupByName(ctx, conn, rs.Primary.ID)

		if err != nil {
			return err
		}

		*v = *output

		return nil
	}
}

func testAccParameterGroupConfig_basic(rName string) string {
	return fmt.Sprintf(`
resource "aws_redshift_parameter_group" "test" {
  name   = %[1]q
  family = "redshift-1.0"

  parameter {
    name  = "require_ssl"
    value = "true"
  }

  parameter {
    name  = "query_group"
    value = "example"
  }

  parameter {
    name  = "enable_user_activity_logging"
    value = "true"
  }
}
`, rName)
}

func testAccParameterGroupConfig_noParameters(rName string) string {
	return fmt.Sprintf(`
resource "aws_redshift_parameter_group" "test" {
  name   = %[1]q
  family = "redshift-1.0"
}
`, rName)
}

func testAccParameterGroupConfig_tags1(rName, tagKey1, tagValue1 string) string {
	return fmt.Sprintf(`
resource "aws_redshift_parameter_group" "test" {
  name        = %[1]q
  family      = "redshift-1.0"
  description = "Test parameter group for terraform"

  tags = {
    %[2]q = %[3]q
  }
}
`, rName, tagKey1, tagValue1)
}

func testAccParameterGroupConfig_tags2(rName, tagKey1, tagValue1, tagKey2, tagValue2 string) string {
	return fmt.Sprintf(`
resource "aws_redshift_parameter_group" "test" {
  name        = %[1]q
  family      = "redshift-1.0"
  description = "Test parameter group for terraform"

  tags = {
    %[2]q = %[3]q
    %[4]q = %[5]q
  }
}
`, rName, tagKey1, tagValue1, tagKey2, tagValue2)
}
