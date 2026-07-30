package grc721

// IGRC721MetadataOnchain follows the OpenSea metadata standard.
type IGRC721MetadataOnchain interface {
	TokenMetadata(tid TokenID) (Metadata, error)
}

type Trait struct {
	DisplayType string
	TraitType   string
	Value       string
}

type Metadata struct {
	Image           string  // URL to the image of the item. Can be any type of image (including SVGs, which will be cached into PNGs by OpenSea), IPFS or Arweave URLs or paths. We recommend using a minimum 3000 x 3000 image.
	ImageData       string  // Raw SVG image data, if you want to generate images on the fly (not recommended). Only use this if you're not including the image parameter.
	ExternalURL     string  // URL that will appear below the asset's image on OpenSea and will allow users to leave OpenSea and view the item on your site.
	Description     string  // Human-readable description of the item. Markdown is supported.
	Name            string  // Name of the item.
	Attributes      []Trait // Attributes for the item, which will show up on the OpenSea page for the item.
	BackgroundColor string  // Background color of the item on OpenSea. Must be a six-character hexadecimal without a pre-pended #
	AnimationURL    string  // URL to a multimedia attachment for the item. Supported file extensions: GLTF, GLB, WEBM, MP4, M4V, OGV, OGG, MP3, WAV, OGA, HTML (for rich experiences and interactive NFTs using JavaScript canvas, WebGL, etc.). Scripts and relative paths within the HTML page are now supported. Access to browser extensions is not supported.
	YoutubeURL      string  // URL to a YouTube video (only used if animation_url is not provided).
}
