## Create buildkitd container

```
# /etc/buildkit/buildkitd.toml

[worker.oci]
  enabled = true
  # no registry mirrors configured

[registry."docker.io"]
  mirrors = []
  http = false
  insecure = false
``` 

Run the buildkitd container in a network where outbound 443/5000 to registries is denied (iptables, a network policy, or --network=none for the build step with only a local registry allowed). Pushes will simply fail. This is the only foolproof method since buildctl build --output type=image,push=true will otherwise attempt to push directly.

Other valid tarball exporters: type=oci,dest=image.tar (OCI layout tarball) and type=tar,dest=fs.tar (raw filesystem). The push-capable exporters are type=image,push=true and type=registry — block those at the wrapper/CI level by rejecting the flags, or by not giving users direct buildctl/buildx access.


https://docs.docker.com/build/buildkit/toml-configuration/





## Logging

Better logging.

When tasks output something explicitly log what gets outputted.
Log when tasks start, what the inputs are, what outputs are

## How to handle Building images?

Add docker ctl to the image? 
pros: easier for developers perhaps?
They simply run cmd="docker build . "
also when running docker build, how is the image stored?
cons: I want to deny developers access to pushing images

### How should build envs be handled?



## Inputs
Im not sure if im 100% satisfied how inputs between tasks are handled.
Its very clunky for example when doing something like `npm install` or `poetry install` 

However I still want the explicit inputs between tasks.
So option could be to allow copy files from other than /out, 

so task npm install:

npm install -> writes to ./node_modules


and task npm run:
inputs = { from npm_install, path ./node_modules } -> mount it at node_modules


This solves the problem diamond pattern problem A -> B1 -> C, A -> B2 -> C because each step will get the context mounted and all required files explicitly needs to be copied over.

A few thing to check though:
- What happends when something already is mounted at that path?

## DSL (.bongo)

Implement parser and schema