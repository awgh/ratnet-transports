# S3 Network Transport

This network transport facilitates the usage of AWS S3, and Amazon S3 Compatibility Object Storage. To use it, you will need:

- A ram node
- A bucket in your S3 compatible API Object Storage to store an object for the monotonic timer
- A bucket in your S3 compatible API Object Storage for the content messages

## How to Play

In order to use this transport, you need to configure several moving pieces. The New function for the S3 Transport is defined as `func New(namespace string, region string, node api.Node, accessKey string, secretKey string, pubkey string, endpoint string, c2Bucket string, timeBucket string)` and requires several arguments to properly instantiate. The arguments to `New` are as follows:

- Namespace: The namespace of your tenancy
- Region: The region in which S3, or compatible S3 API, will be used
- Node: The type of node being used (ram node recommended)
- AccessKey: Your S3 API access key
- SecretKey: Your S3 API secret key
- PubKey: The public routing key for the transport
- EndPoint: The endpoint in which to use for S3. This matters if you will use a non-AWS S3 bucket and is used for S3 API compatibility.
- C2Bucket: The bucket in which content messages are picked up and dropped off into
- TimeBucket: The bucket in which the monotonic time counter is placed in

### EndPoint

If you are not using AWS, you will need to define your S3 endpoint. Several cloud vendors have documentation of how to do this:

1. [GCP](https://cloud.google.com/storage/docs/request-endpoints)
1. [OCI](https://docs.cloud.oracle.com/en-us/iaas/Content/Object/Tasks/s3compatibleapi.htm)
