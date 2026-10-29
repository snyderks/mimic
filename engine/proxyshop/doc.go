// Package proxyshop converts Proxyshop's Photoshop templates into mimic
// templates.
//
// A conversion has two steps joined by a recipe, a JSON file that says which
// layers of which PSD become which part of the template, and what the
// template's manifest and layouts hold. Analyze reads PSDs and writes a recipe
// by following the conventions Proxyshop's templates share. Build reads a
// recipe and its PSDs and writes the template's source folder, which the pack
// command bundles. Convert runs both. Because the recipe sits between them, a
// conversion can be read, kept in version control, and adjusted by hand before
// it is built, and a hand made recipe builds the same way as one Analyze wrote.
//
// Proxyshop renders a card by showing and hiding layers of its PSD: each frame
// element is a group, such as "Pinlines & Textbox", holding a hidden layer per
// color, such as "W" or "WU", and a card's colors pick which to show. Analyze
// turns each such group into a part with a variant per color, and reads
// conditional groups such as "Legendary Crown" and "Nyx", the border swap for
// legendary cards, the type layers' positions and sizes, and the reference
// boxes for the art, rules text and set symbol into the layout.
//
// Build draws each variant through the psdrender compositor, the way Photoshop
// would show the document with that layer on, so blend modes, masks, clipping
// and layer styles are baked into the image the template stores. A part whose
// pair images match a blend of its single color images within a pixel or two
// keeps only the single colors and a fitted blend, which the engine mixes at
// render time.
//
// Build also reads recipes that cut layers directly and recreate effects with
// measured parameters, which is how the M15 template was first made, so that
// template rebuilds from its PSDs byte for byte.
package proxyshop
