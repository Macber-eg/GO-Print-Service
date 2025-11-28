# Fonts Directory

This directory can contain custom fonts for the badge generator.

## Default Fonts

The Docker image includes these system fonts:
- DejaVu Sans (default)
- Liberation Sans
- Liberation Serif
- Liberation Mono

## Adding Custom Fonts

To add custom fonts (like Arial):

1. Place `.ttf` files in this directory:
   - `arial.ttf` - Regular
   - `arialbd.ttf` - Bold

2. Rebuild and deploy

## Note

If custom fonts are not found, the system will fall back to Helvetica (built-in PDF font).
