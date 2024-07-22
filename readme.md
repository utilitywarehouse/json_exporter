# json_exporter

A prometheus exporter which scrapes JSON objects to collect metrics.
Inspired by [json_exporter](https://github.com/prometheus-community/json_exporter)
but using [jq expression](https://jqlang.github.io/jq/manual/) instead of JSONPath and webhook for the source.


## Example Usage
### 1. configure and run json_exporter with [test config](test/config.yaml)

```sh
export TEST_SHARED_WEB_HOOK_KEY="some-secret"
export EXPORTER_CONFIG=test/config.yaml
go build && ./json_exporter
```

### 2. send json payload to exporter's webhooks

```sh
# send data to example webhook with secret
> curl -i -d @test/data.json \
    -H "Authorization:some-secret" \
    http://localhost:9000/webhook/example

HTTP/1.1 200 OK
Access-Control-Allow-Origin: *
Date: Mon, 01 Jan 2024 22:37:06 GMT
Content-Length: 2
Content-Type: text/plain; charset=utf-8

ok

# send data to animals webhook
> curl -i -d @test/animal-data.json \
    http://localhost:9000/animals

HTTP/1.1 204 No Content
Date: Mon, 01 Jan 2024 22:37:06 GMT

```

### 3. verify metrics on exporter

```sh
❯ curl http://localhost:9000/metrics

# HELP animal_population Example of top-level lists in a separate collectors
# TYPE animal_population gauge
animal_population{name="deer",predator="false"} 456
animal_population{name="lion",predator="true"} 123
animal_population{name="pigeon",predator="false"} 789
# HELP example_global_value Example of a top-level global value scrape in the json
# TYPE example_global_value gauge
example_global_value{environment="beta",location="planet-mars"} 1234
# HELP example_inactive_value_count Example of a timestamped value scrape in the json
# TYPE example_inactive_value_count counter
example_inactive_value_count{environment="beta"} 2
# HELP example_value_active Example of sub-level value scrapes from a json
# TYPE example_value_active counter
example_value_active{environment="beta",id="id-A"} 1
example_value_active{environment="beta",id="id-C"} 1
# HELP example_value_boolean Example of sub-level value scrapes from a json
# TYPE example_value_boolean counter
example_value_boolean{environment="beta",id="id-A"} 1
example_value_boolean{environment="beta",id="id-C"} 0
# HELP example_value_count Example of sub-level value scrapes from a json
# TYPE example_value_count counter
example_value_count{environment="beta",id="id-A"} 1
example_value_count{environment="beta",id="id-C"} 3
```

## Collector Config

```yaml
collectors:
  # id of the collector
  example: 
    # namespace for all metrics of the collector
    namespace: example
    # labels shared with all metrics of the collector 
    defaultLabels: 
        # name of the label
      - name: environment
        # value (jq expression): evaluated on json object of metric
        # its always relative to metric's 'path' exp
        value: '"beta"'
    # list of all the metrics of the collector
    metrics:
      - name: global_value
        help: Example of a top-level global value scrape in the json
        # type of the metrics value should be either 'counter' or 'gauge'
        # default is counter
        type: gauge
        # path (jq expression): path exp for the json object on which this metrics should be collected
        # default is '.'
        path: "."
        # filter (jq expression): metric collection will be skipped if this
        # exp results in 'false' value
        filter: "."
        # value (jq expression): should result in the value of the metric
        # default is 1
        value: .counter
        # 'operation' is only used for gauge metrics
        # value should be either 'set' or 'add', default is 'set'
        # 'set' sets the Gauge to an given value.
        # 'add' adds the given value to the Gauge. (The value can be negative,
        # resulting in a decrease of the Gauge.)
        operation: set
        # labels specific to this metric
        labels:
          - name: location
            value: '"planet-"+ .location'

      - name: inactive_value_count
        help: Example of a timestamped value scrape in the json
        path: .values[]
        filter: '.state == "INACTIVE"'
        value: ".count"

  animals:
    defaultLabels:
      - name: name
        value: .noun
    metrics:
      - name: animal_population
        help: Example of top-level lists in a separate collectors
        type: gauge
        path: ".[]"
        value: .population
        labels:
          - name: predator
            value: .predator
```
### Notes:
* at the moment only `counter` and `gauge` metrics are supported. if metric type
  is counter given `value` will be `added` to the metrics, for gauge value will
  be `set`. 
* to set const value use `value: '"beta"'` for this exp value will always be `beta`
* jq [doesn't support the "decimal fraction" in timestamp](https://github.com/jqlang/jq/issues/2224). to truncate use `| .[0:19] +"Z" | fromdateiso8601`..
  
  `jq -nr '"2020-12-11T01:00:52.605001Z" | .[0:19] +"Z" | fromdateiso8601'`



## Webhook Config

```yaml
webhooks:
  # id of the webhook
  example:
    # allowed method for the HTTP request.
    method: POST
    # The URL path on which requests are sent.
    path: /webhook/example
    auth:
      # A list of HTTP headers and values all request must have
      headers:
        - name: Authorization
          valueFromEnv: TEST_SHARED_WEB_HOOK_KEY
        - name: Content-Type
          value: application/json
    # Specifies the HTTP response that will be returned on successful requests.
    response:
      code: 200
      headers:
        - name: Access-Control-Allow-Origin
          value: "*"
      message: "ok"
    # list of collectors where received payload will be sent
    collectors:
        # id of the collector
      - id: example
        # transform is a jq expression which will be executed on payload and 
        # output will be sent to collector
        transform: "."

  animals:
    method: POST
    path: /animals
    response:
      code: 204
    collectors:
      - id: animals

```

Note:
* webhooks will use same port as metrics server