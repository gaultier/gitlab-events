# Gitlab events

Toy cli to continuously watch gitlab events for multiple projects, because Gitlab notifications suck.

```
go build
// Watch projects with id 11, 15, and 100
./gitlab-events -token="$GITLAB_TOKEN" 11 15 100
```


## LICENSE
MIT
