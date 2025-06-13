# ClusterLoader2 Dependency System

The dependency system in ClusterLoader2 allows you to install and manage cluster dependencies as part of your test configuration. Dependencies are now executed at the test level with explicit setup and teardown phases, providing better lifecycle management and error handling.

## Overview

Dependencies are executed in two phases:
- **Setup Phase**: Dependencies are set up before any test steps begin execution
- **Teardown Phase**: Dependencies are torn down after all test resources are cleaned up

Key features:
- Dependencies run before steps and are torn down after cleanup
- Timeout enforcement for both setup and teardown operations  
- If any dependency fails, the entire test workflow fails
- Dependencies are torn down in reverse order of setup

## New Usage Format

Dependencies are now defined at the test level using the new name/method format:

```yaml
name: dra-steady-state
dependencies:
- name: install dra driver
  method: DRAInstall 
  timeout: 10m
  params:
    foo: bar
steps:
- name: step1
  # ... step configuration
```

### Dependency Fields

- `name` (required): A unique identifier for this dependency instance
- `method` (required): The dependency method registered in the factory
- `timeout` (optional): Maximum duration for both setup and teardown. If 0, operations wait forever
- `params` (optional): Parameters passed to the dependency

## Creating Custom Dependencies

To create a custom dependency with the new interface:

1. Create a new package under `pkg/dependency/`
2. Implement the new `Dependency` interface:
   ```go
   type Dependency interface {
       Setup(config *Config) error
       Teardown(config *Config) error
       String() string
   }
   ```

3. Example implementation:
   ```go
   type myDependency struct{}
   
   func (d *myDependency) Setup(config *Config) error {
       // Setup logic here
       return nil
   }
   
   func (d *myDependency) Teardown(config *Config) error {
       // Teardown logic here  
       return nil
   }
   
   func (d *myDependency) String() string {
       return "MyDependency"
   }
   ```

4. Register your dependency in an `init()` function:
   ```go
   func init() {
       if err := dependency.Register("MyDependency", createMyDependency); err != nil {
           klog.Fatalf("Cannot register %s: %v", "MyDependency", err)
       }
   }
   ```

5. Import your dependency package in `cmd/clusterloader.go`