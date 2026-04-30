# API contracts

## 1. Resource request

URL:
```bash
/dd/{id}/{url}
```

Body: 
```json
{
    "content": "content"
}
```

Header:
```json
{
    "Authorization": "token"
}
```

## 2. Stream distribution request

```json
{
    "id": "id of the resource peer",
    "url": "stream url",
    "token": "access token"
}
```

## 3. 