
```sh
curl -s http://localhost:3000/package/react/16.13.0
```

    go/api_npm_packages % [main] > curl -s http://localhost:3000/package/react/16.13.0
    go/api_npm_packages % [main] > 

```sh
curl http://localhost:3000/package/react/16.13.0
```

    go/api_npm_packages % [main] > curl  http://localhost:3000/package/react/16.13.0  
    curl: (7) Failed to connect to localhost port 3000 after 0 ms: Couldn't connect to server

```sh
curl http://localhost:3003/package/react/16.13.0 
```

You can run the tests with:

```sh
go test ./... -v       
```
 go/api_npm_packages % [main] > go test ./... -v       
?   	github.com/zen37/npm_packages	[no test files]
=== RUN   TestPackageHandler
2024/08/11 18:37:22 Successfully handled request for package: react, version: 16.13.0
--- PASS: TestPackageHandler (1.91s)
PASS
ok  	github.com/zen37/npm_packages/api	(cached)