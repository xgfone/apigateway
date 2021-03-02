# apigateway [Work In Progress]

`apigateway` is the api gateway binary program based on the api gateway library [apigw](https://github.com/xgfone/apigw).

    ```shell
    # Environment: 8C8GB, NIC 1000Mb/s, CentOS 7.4.1708 64bit, Go1.16
    $ wrk -t 8 -c 1000 -d 30s --latency -H 'Host: www.exampletest.com' http://192.168.1.10/v1/test
    Running 30s test @ http://192.168.1.10/v1/test
      8 threads and 1000 connections
      Thread Stats   Avg      Stdev     Max   +/- Stdev
        Latency    39.04ms   21.66ms 331.90ms   73.64%
        Req/Sec     3.17k    428.99    8.31k    84.21%
      Latency Distribution
         50%   35.77ms
         75%   50.02ms
         90%   66.15ms
         99%  103.17ms
      756985 requests in 30.01s, 109.73MB read
      Socket errors: connect 0, read 0, write 231, timeout 0
    Requests/sec:  25223.57
    Transfer/sec:  3.66MB

    # Environment: 32Core, NIC 1000Mb/s, CentOS 7.4.1708 64bit, Go1.16
    $ wrk -t 8 -c 1000 -d 30s --latency -H 'Host: www.exampletest.com' http://192.168.1.4/v1/test
    Running 30s test @ http://192.168.1.4/v1/test
      8 threads and 1000 connections
      Thread Stats   Avg      Stdev     Max   +/- Stdev
        Latency    11.49ms    7.63ms 123.42ms   67.45%
        Req/Sec    10.95k     1.28k   26.59k    76.69%
      Latency Distribution
         50%   10.99ms
         75%   16.28ms
         90%   21.40ms
         99%   31.24ms
      2616921 requests in 30.09s, 379.34MB read
    Requests/sec:  86958.90
    Transfer/sec:  12.61MB
    ````
