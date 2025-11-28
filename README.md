# Badge PDF Generator Service

A high-performance Go service for generating PDF badges from templates. Designed for event management systems with support for 500+ concurrent requests.

## 🚀 Performance

| Metric | Value |
|--------|-------|
| Single badge generation | ~50-80ms |
| 500 concurrent requests | ~2-3 seconds |
| Memory usage | ~500MB - 1GB |
| Max throughput | 200-400 badges/second |

## 📋 Features

- **Native PDF Generation** - No Chrome/browser required, pure Go
- **Template Caching** - Background images cached for instant reuse
- **Image Pre-fetching** - Parallel download of user photos
- **QR Code Generation** - Built-in QR code support
- **Batch Processing** - Generate multiple badges in one request
- **Base64 or Binary Output** - Flexible response formats

## 🛠 Deployment on Railway

### Step 1: Create GitHub Repository

1. Create a new repository on GitHub
2. Push this code to your repository:

```bash
git init
git add .
git commit -m "Initial commit"
git branch -M main
git remote add origin https://github.com/YOUR_USERNAME/badge-service.git
git push -u origin main
```

### Step 2: Deploy on Railway

1. Go to [railway.app](https://railway.app)
2. Click **"New Project"**
3. Select **"Deploy from GitHub repo"**
4. Choose your `badge-service` repository
5. Railway will automatically detect the Dockerfile and deploy

### Step 3: Get Your URL

After deployment, Railway will provide you with a URL like:
```
https://badge-service-production-xxxx.up.railway.app
```

That's it! Your service is now live.

## 📖 API Documentation

### Health Check

```
GET /health
```

Response:
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "uptime": "2h30m15s"
}
```

### Generate Single Badge

```
POST /api/badge/generate
Content-Type: application/json
```

Request Body:
```json
{
  "template": {
    "id": 9,
    "design": {
      "layers": [...],
      "settings": {
        "paperWidth": 210,
        "paperHeight": 297,
        "dpi": 300
      }
    },
    "assets": {
      "asset_0": "https://your-s3.com/background.png"
    }
  },
  "user": {
    "user": {
      "id": "user-123",
      "identifier": "7882919302",
      "customFieldValues": [
        {
          "fieldId": "4dd704b2-9aa2-4651-b8eb-0e508b6a19e5",
          "value": "Mr"
        },
        {
          "fieldId": "de4a02ae-33b6-4cf3-afc8-488fa09504df",
          "value": "John"
        },
        {
          "fieldId": "cfd8df63-7bfc-4935-a7c1-3b37d2ccca9e",
          "value": "Doe"
        }
      ]
    }
  }
}
```

**Response (Binary PDF):**
- Returns PDF file directly
- Content-Type: `application/pdf`

**Response (JSON with Base64):**
Add header: `Accept: application/json`
```json
{
  "success": true,
  "pdf_base64": "JVBERi0xLjcK...",
  "filename": "badge_7882919302.pdf"
}
```

### Generate Batch (Multiple Badges)

```
POST /api/badge/batch
Content-Type: application/json
```

Request Body:
```json
{
  "template": {
    "id": 9,
    "design": { ... },
    "assets": { ... }
  },
  "users": [
    { "user": { "id": "user-1", ... } },
    { "user": { "id": "user-2", ... } },
    { "user": { "id": "user-3", ... } }
  ]
}
```

Response:
```json
{
  "success": true,
  "total": 3,
  "results": [
    {
      "user_id": "user-1",
      "identifier": "123456",
      "success": true,
      "pdf_base64": "JVBERi0xLjcK..."
    },
    {
      "user_id": "user-2",
      "identifier": "789012",
      "success": true,
      "pdf_base64": "JVBERi0xLjcK..."
    }
  ]
}
```

### Preload Template (Optional Optimization)

Pre-cache template assets before generating badges:

```
POST /api/template/preload
Content-Type: application/json
```

```json
{
  "template": {
    "assets": {
      "asset_0": "https://your-s3.com/background.png"
    }
  }
}
```

### Cache Management

Get cache statistics:
```
GET /api/cache/stats
```

Clear cache:
```
POST /api/cache/clear
```

## 🎨 Template Structure

Templates use a layer-based design system:

```json
{
  "design": {
    "layers": [
      {
        "id": "layer-1",
        "type": "image",
        "content": "asset_0",
        "position": { "x": 0, "y": 0 },
        "size": { "width": 210, "height": 297 },
        "visible": true,
        "zIndex": 0
      },
      {
        "id": "layer-2",
        "type": "text",
        "content": "{{customFields.de4a02ae-33b6-4cf3-afc8-488fa09504df}}",
        "position": { "x": 50, "y": 100 },
        "size": { "width": 100, "height": 20 },
        "style": {
          "fontSize": 24,
          "fontFamily": "Arial",
          "fontWeight": "bold",
          "color": "#000000",
          "textAlign": "center"
        },
        "visible": true,
        "zIndex": 1
      },
      {
        "id": "layer-3",
        "type": "qrcode",
        "position": { "x": 80, "y": 200 },
        "size": { "width": 50, "height": 50 },
        "visible": true,
        "zIndex": 2
      },
      {
        "id": "layer-4",
        "type": "image",
        "dataBinding": "customFields.d1c3dc73-86ef-47b0-af79-bc5047f2c100",
        "position": { "x": 20, "y": 50 },
        "size": { "width": 40, "height": 50 },
        "visible": true,
        "zIndex": 3
      }
    ],
    "settings": {
      "paperWidth": 210,
      "paperHeight": 297,
      "dpi": 300
    }
  }
}
```

### Layer Types

| Type | Description |
|------|-------------|
| `image` | Static image (from assets) or dynamic (from user data via dataBinding) |
| `text` | Text with placeholder support `{{customFields.xxx}}` |
| `qrcode` | QR code generated from user identifier |
| `container` | Container for grouped elements with flex layout |
| `shape` | Rectangle with background color |

## 🔧 Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 3000 | Server port |
| `CACHE_DIR` | /tmp/badge-cache | Directory for cached files |

## 📊 Integration Example (Node.js/PHP)

### Node.js

```javascript
const axios = require('axios');
const fs = require('fs');

async function generateBadge(template, userData) {
  const response = await axios.post(
    'https://your-service.railway.app/api/badge/generate',
    { template, user: userData },
    { 
      responseType: 'arraybuffer',
      headers: { 'Content-Type': 'application/json' }
    }
  );
  
  fs.writeFileSync('badge.pdf', response.data);
  console.log('Badge saved!');
}

// Or get as base64:
async function generateBadgeBase64(template, userData) {
  const response = await axios.post(
    'https://your-service.railway.app/api/badge/generate',
    { template, user: userData },
    { 
      headers: { 
        'Content-Type': 'application/json',
        'Accept': 'application/json'
      }
    }
  );
  
  return response.data.pdf_base64;
}
```

### PHP

```php
<?php

function generateBadge($template, $userData) {
    $url = 'https://your-service.railway.app/api/badge/generate';
    
    $payload = json_encode([
        'template' => $template,
        'user' => $userData
    ]);
    
    $ch = curl_init($url);
    curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
    curl_setopt($ch, CURLOPT_POST, true);
    curl_setopt($ch, CURLOPT_POSTFIELDS, $payload);
    curl_setopt($ch, CURLOPT_HTTPHEADER, [
        'Content-Type: application/json',
        'Accept: application/json'
    ]);
    
    $response = curl_exec($ch);
    curl_close($ch);
    
    $data = json_decode($response, true);
    return base64_decode($data['pdf_base64']);
}

// Save to file
$pdf = generateBadge($template, $userData);
file_put_contents('badge.pdf', $pdf);
```

## 🐛 Troubleshooting

### Common Issues

1. **Images not loading**
   - Ensure URLs are publicly accessible
   - Check if CORS is enabled on your S3 bucket

2. **Text not appearing**
   - Verify placeholder format: `{{customFields.FIELD_ID}}`
   - Check that fieldId in user data matches template placeholders

3. **Slow first request**
   - First request downloads and caches template assets
   - Use `/api/template/preload` to pre-cache

4. **Memory issues on batch**
   - Reduce batch size (max 500)
   - Ensure Railway instance has enough RAM

## 📜 License

MIT License
