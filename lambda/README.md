# shimmy-sandbox Lambda Handler

Runs untrusted code (Python 3, C, shell) inside a shimmy-sandbox via AWS Lambda.

## Prerequisites

1. Build and publish the Lambda Layer (contains the sandbox binary + DynamoRIO):

```bash
# From repo root
bash scripts/build-lambda-layer.sh

# Publish layer
aws lambda publish-layer-version \
  --layer-name shimmy-sandbox \
  --zip-file fileb://dist/lambda-layer.zip \
  --compatible-runtimes provided.al2023 \
  --compatible-architectures x86_64
```

Note the `LayerVersionArn` in the output.

## Build the handler binary

```bash
cd lambda
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bootstrap .
zip handler.zip bootstrap
```

## Deploy with AWS CLI

```bash
# Create function (first time)
aws lambda create-function \
  --function-name shimmy-sandbox-runner \
  --runtime provided.al2023 \
  --architectures x86_64 \
  --handler bootstrap \
  --role arn:aws:iam::<account-id>:role/<execution-role> \
  --zip-file fileb://handler.zip \
  --layers <LayerVersionArn> \
  --timeout 30 \
  --memory-size 512

# Update code only
aws lambda update-function-code \
  --function-name shimmy-sandbox-runner \
  --zip-file fileb://handler.zip

# Update layers
aws lambda update-function-configuration \
  --function-name shimmy-sandbox-runner \
  --layers <LayerVersionArn>
```

## Deploy with AWS SAM

```yaml
# template.yaml
AWSTemplateFormatVersion: '2010-09-09'
Transform: AWS::Serverless-2016-10-31

Globals:
  Function:
    Timeout: 30
    MemorySize: 512
    Architectures: [x86_64]

Resources:
  SandboxLayer:
    Type: AWS::Serverless::LayerVersion
    Properties:
      LayerName: shimmy-sandbox
      ContentUri: ../dist/lambda-layer.zip
      CompatibleRuntimes: [provided.al2023]
      CompatibleArchitectures: [x86_64]

  RunnerFunction:
    Type: AWS::Serverless::Function
    Properties:
      FunctionName: shimmy-sandbox-runner
      Runtime: provided.al2023
      Handler: bootstrap
      CodeUri: .
      Layers:
        - !Ref SandboxLayer
      Policies:
        - AWSLambdaBasicExecutionRole
```

```bash
sam build && sam deploy --guided
```

## Invocation

```json
{
  "language": "python3",
  "code": "print('hello')",
  "stdin": "",
  "timeout_ms": 5000
}
```

Response:

```json
{
  "stdout": "hello\n",
  "stderr": "",
  "exit_code": 0,
  "time_ms": 42,
  "sandbox": "dynamorio",
  "verdict": "OK"
}
```

Verdicts: `OK` (exit 0), `TLE` (exit 124), `SB` (exit 126, sandbox blocked), `RE` (any other non-zero).
