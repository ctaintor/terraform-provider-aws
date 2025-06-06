---
subcategory: "CloudFront"
layout: "aws"
page_title: "AWS: aws_cloudfront_distribution"
description: |-
  Provides a CloudFront web distribution data source.
---


<!-- Please do not edit this file, it is generated. -->
# Data Source: aws_cloudfront_distribution

Use this data source to retrieve information about a CloudFront distribution.

## Example Usage

```typescript
// DO NOT EDIT. Code generated by 'cdktf convert' - Please report bugs at https://cdk.tf/bug
import { Construct } from "constructs";
import { TerraformStack } from "cdktf";
/*
 * Provider bindings are generated by running `cdktf get`.
 * See https://cdk.tf/provider-generation for more details.
 */
import { DataAwsCloudfrontDistribution } from "./.gen/providers/aws/data-aws-cloudfront-distribution";
class MyConvertedCode extends TerraformStack {
  constructor(scope: Construct, name: string) {
    super(scope, name);
    new DataAwsCloudfrontDistribution(this, "test", {
      id: "EDFDVBD632BHDS5",
    });
  }
}

```

## Argument Reference

This data source supports the following arguments:

* `id` - Identifier for the distribution. For example: `EDFDVBD632BHDS5`.

## Attribute Reference

This data source exports the following attributes in addition to the arguments above:

* `id` - Identifier for the distribution. For example: `EDFDVBD632BHDS5`.

* `aliases` - List that contains information about CNAMEs (alternate domain names), if any, for this distribution.

* `arn` - ARN (Amazon Resource Name) for the distribution. For example: arn:aws:cloudfront::123456789012:distribution/EDFDVBD632BHDS5, where 123456789012 is your AWS account ID.

* `status` - Current status of the distribution. `Deployed` if the
    distribution's information is fully propagated throughout the Amazon
    CloudFront system.

* `domainName` - Domain name corresponding to the distribution. For
    example: `d604721fxaaqy9.cloudfront.net`.

* `lastModifiedTime` - Date and time the distribution was last modified.

* `inProgressValidationBatches` - The number of invalidation batches
    currently in progress.

* `etag` - Current version of the distribution's information. For example:
    `E2QWRUHAPOMQZL`.

* `hostedZoneId` - CloudFront Route 53 zone ID that can be used to
     route an [Alias Resource Record Set][7] to. This attribute is simply an
     alias for the zone ID `Z2FDTNDATAQYW2`.
* `webAclId` AWS WAF web ACL associated with this distribution.

<!-- cache-key: cdktf-0.20.8 input-7b44ab0087c4af7f48dfdb8564eebc07afbeb6c72c98e64a25a0fc3fe8f29955 -->